package server

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type principal struct {
	userID   string
	clientID string
	groups   []string
}

type principalKey struct{}

func principalFrom(ctx context.Context) (principal, bool) {
	p, ok := ctx.Value(principalKey{}).(principal)
	return p, ok
}

type tokenVerifier struct {
	issuer, audience, groupsClaim string
	client                        *http.Client
	mu                            sync.Mutex
	keys                          map[string]*rsa.PublicKey
	validUntil                    time.Time
	lastRefresh                   time.Time
}

func newTokenVerifier(issuer, audience, groupsClaim string) *tokenVerifier {
	return &tokenVerifier{issuer: issuer, audience: audience, groupsClaim: groupsClaim, client: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (v *tokenVerifier) verify(ctx context.Context, token string) (principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32768 {
		return principal{}, errors.New("invalid token format")
	}
	decode := func(value string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(value) }
	headerBytes, err := decode(parts[0])
	if err != nil {
		return principal{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "RS256" || header.Kid == "" {
		return principal{}, errors.New("unsupported token header")
	}
	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return principal{}, err
	}
	sig, err := decode(parts[2])
	if err != nil {
		return principal{}, err
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], sig); err != nil {
		return principal{}, errors.New("invalid token signature")
	}
	claimsBytes, err := decode(parts[1])
	if err != nil {
		return principal{}, err
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return principal{}, err
	}
	var iss, sub, clientID string
	var exp, nbf, iat int64
	_ = json.Unmarshal(claims["iss"], &iss)
	_ = json.Unmarshal(claims["sub"], &sub)
	_ = json.Unmarshal(claims["azp"], &clientID)
	if err := json.Unmarshal(claims["exp"], &exp); err != nil {
		return principal{}, errors.New("missing expiration")
	}
	now := time.Now().Unix()
	if iss != v.issuer || sub == "" || exp <= now {
		return principal{}, errors.New("invalid issuer, subject, or expiration")
	}
	if raw := claims["nbf"]; len(raw) > 0 {
		if json.Unmarshal(raw, &nbf) != nil || nbf > now {
			return principal{}, errors.New("token not yet valid")
		}
	}
	if raw := claims["iat"]; len(raw) > 0 {
		if json.Unmarshal(raw, &iat) != nil || iat > now {
			return principal{}, errors.New("token issued in future")
		}
	}
	var audiences []string
	if err := json.Unmarshal(claims["aud"], &audiences); err != nil {
		var audience string
		if json.Unmarshal(claims["aud"], &audience) != nil {
			return principal{}, errors.New("invalid audience")
		}
		audiences = []string{audience}
	}
	found := false
	for _, audience := range audiences {
		if audience == v.audience {
			found = true
		}
	}
	if !found {
		return principal{}, errors.New("wrong audience")
	}
	var groups []string
	if raw := claims[v.groupsClaim]; len(raw) > 0 && json.Unmarshal(raw, &groups) != nil {
		return principal{}, errors.New("invalid groups claim")
	}
	return principal{userID: iss + "|" + sub, clientID: clientID, groups: groups}, nil
}

func (v *tokenVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Now().Before(v.validUntil) {
		if key := v.keys[kid]; key != nil {
			return key, nil
		}
		// Refresh for key rotation, but bound repeated unknown-kid lookups.
		if time.Since(v.lastRefresh) < 30 * time.Second {
			return nil, errors.New("unknown signing key")
		}
	}
	discoveryURL := strings.TrimRight(v.issuer, "/") + "/.well-known/openid-configuration"
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.fetch(ctx, discoveryURL, &discovery); err != nil {
		return nil, err
	}
	issuerURL, _ := url.Parse(v.issuer)
	jwksURL, err := url.Parse(discovery.JWKSURI)
	if err != nil || discovery.Issuer != v.issuer || jwksURL.Scheme != issuerURL.Scheme || jwksURL.Host != issuerURL.Host {
		return nil, errors.New("untrusted JWKS location")
	}
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := v.fetch(ctx, discovery.JWKSURI, &jwks); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, item := range jwks.Keys {
		if item.Kid == "" || item.Kty != "RSA" || (item.Alg != "" && item.Alg != "RS256") || (item.Use != "" && item.Use != "sig") {
			continue
		}
		n, nerr := base64.RawURLEncoding.DecodeString(item.N)
		e, eerr := base64.RawURLEncoding.DecodeString(item.E)
		if nerr != nil || eerr != nil || len(n) < 256 || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		if exponent < 3 || exponent > 2147483647 || exponent%2 == 0 {
			continue
		}
		keys[item.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
	}
	v.keys, v.validUntil = keys, time.Now().Add(5 * time.Minute)
	v.lastRefresh = time.Now()
	if key := keys[kid]; key != nil {
		return key, nil
	}
	return nil, errors.New("unknown signing key")
}

func (v *tokenVerifier) fetch(ctx context.Context, location string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return err
	}
	response, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("issuer discovery returned %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1 << 20)).Decode(target)
}
