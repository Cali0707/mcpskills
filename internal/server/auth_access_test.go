package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cali0707/mcpskills/internal/config"
	"github.com/Cali0707/mcpskills/internal/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type bearerTransport string

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(copy)
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test"})
	data, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(data)
	hash := sha256.Sum256([]byte(encoded))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestProtectedSkillsAndResourceDiscovery(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	issuerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/realms/test/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/protocol/openid-connect/certs"})
		case "/realms/test/protocol/openid-connect/certs":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{
				"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuerServer.Close()
	issuer = issuerServer.URL + "/realms/test"
	source := fixtureCatalog(t)
	file := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(file, []byte("skills:\n  'skill://toolbox/nested-tool/SKILL.md':\n    anyGroup: [engineering]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Load(file, source)
	if err != nil {
		t.Fatal(err)
	}
	access, err := newAccessControl(source, rules)
	if err != nil {
		t.Fatal(err)
	}
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer meterProvider.Shutdown(context.Background())
	reads, err := meterProvider.Meter("test").Int64Counter("mcpskills.resource.reads")
	if err != nil {
		t.Fatal(err)
	}
	access.telemetry = &telemetry{tracer: provider.Tracer("test"), readCounter: reads}
	mcpServer, err := buildMCPServer(source, access, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resourceURL string
	mcpHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := config.Config{EndpointPath: "/mcp", ResourceURL: resourceURL, Issuer: issuer}
		protectedHTTPHandler(mcpServer, cfg, newTokenVerifier(issuer, resourceURL, "groups"), nil).ServeHTTP(w, r)
	}))
	defer mcpHTTP.Close()
	resourceURL = mcpHTTP.URL + "/mcp"
	resp, err := http.Get(mcpHTTP.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if metadata["resource"] != resourceURL || resp.StatusCode != 200 {
		t.Fatalf("metadata = %v, status = %d", metadata, resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, resourceURL, strings.NewReader("{}"))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("missing OAuth challenge: %d %v", resp.StatusCode, resp.Header)
	}

	claims := func(groups []string) map[string]any {
		return map[string]any{
			"iss": issuer, "sub": "user-1", "aud": resourceURL, "exp": time.Now().Add(time.Hour).Unix(), "groups": groups, "azp": "pi",
		}
	}
	for _, tc := range []struct {
		name   string
		groups []string
		want   int
	}{
		{"engineering", []string{"engineering"}, 2}, {"other", []string{"sales"}, 1}, {"no-group", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			_ = mcp.AddSendingCustomMethod[*listParams, *listSkillsResult](client, "skills/list")
			_ = mcp.AddSendingCustomMethod[*uriParams, *getSkillResult](client, "skills/get")
			_ = mcp.AddSendingCustomMethod[*directoryParams, *directoryResult](client, "resources/directory/read")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: resourceURL, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport(signedToken(t, key, claims(tc.groups)))}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			listed, err := mcp.CallCustomMethod[*listParams, *listSkillsResult](ctx, session, "skills/list", &listParams{})
			if err != nil || len(listed.Skills) != tc.want || listed.CacheScope != "private" {
				t.Fatalf("list = %+v, %v", listed, err)
			}
			resources, err := session.ListResources(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range resources.Resources {
				if tc.want == 1 && strings.Contains(item.URI, "nested-tool") {
					t.Errorf("restricted resource listed: %s", item.URI)
				}
			}
			dir, err := mcp.CallCustomMethod[*directoryParams, *directoryResult](ctx, session, "resources/directory/read", &directoryParams{URI: "skill://toolbox"})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range dir.Resources {
				if tc.want == 1 && strings.Contains(item.URI, "nested-tool") {
					t.Errorf("restricted directory listed: %s", item.URI)
				}
			}
			if _, err := mcp.CallCustomMethod[*uriParams, *getSkillResult](ctx, session, "skills/get", &uriParams{URI: "skill://toolbox/SKILL.md"}); err != nil {
				t.Fatalf("get skill: %v", err)
			}
			for _, uri := range []string{"skill://toolbox/SKILL.md", "skill://toolbox/references/guide.md"} {
				if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri}); err != nil {
					t.Fatalf("read %s: %v", uri, err)
				}
			}
			if tc.want == 2 {
				if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "skill://toolbox/nested-tool/SKILL.md"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "skill://toolbox/missing.md"}); err == nil {
				t.Error("missing resource read succeeded")
			}
			if tc.want == 1 {
				uri := "skill://toolbox/nested-tool/SKILL.md"
				if _, err := mcp.CallCustomMethod[*uriParams, *getSkillResult](ctx, session, "skills/get", &uriParams{URI: uri}); err == nil {
					t.Error("restricted get succeeded")
				}
				if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri}); err == nil {
					t.Error("restricted read succeeded")
				}
				if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "skill://toolbox/nested-tool/examples/sample.txt"}); err == nil {
					t.Error("restricted supporting file read succeeded")
				}
				if _, err := mcp.CallCustomMethod[*directoryParams, *directoryResult](ctx, session, "resources/directory/read", &directoryParams{URI: "skill://toolbox/nested-tool"}); err == nil {
					t.Error("restricted directory read succeeded")
				}
			}
		})
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int64)
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "mcpskills.resource.reads" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("read metric type = %T", item.Data)
			}
			for _, point := range sum.DataPoints {
				uri, ok := point.Attributes.Value("mcp.resource.uri")
				if !ok {
					t.Fatal("read metric has no resource URI")
				}
				skill, ok := point.Attributes.Value("mcp.skill.uri")
				if !ok {
					t.Fatal("read metric has no owning skill URI")
				}
				wantSkill := "skill://toolbox/SKILL.md"
				if strings.Contains(uri.AsString(), "/nested-tool/") {
					wantSkill = "skill://toolbox/nested-tool/SKILL.md"
				}
				if skill.AsString() != wantSkill {
					t.Errorf("read %s attributed to %s, want %s", uri.AsString(), skill.AsString(), wantSkill)
				}
				revision, ok := point.Attributes.Value("skill.source.revision")
				if !ok || revision.AsString() != source.Revision() {
					t.Errorf("read %s revision = %s, want %s", uri.AsString(), revision.AsString(), source.Revision())
				}
				counts[uri.AsString()] = point.Value
			}
		}
	}
	want := map[string]int64{"skill://toolbox/SKILL.md": 3, "skill://toolbox/references/guide.md": 3, "skill://toolbox/nested-tool/SKILL.md": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("read counts = %v, want %v", counts, want)
	}
	for _, invalid := range []map[string]any{
		{"iss": issuer, "sub": "x", "aud": "wrong", "exp": time.Now().Add(time.Hour).Unix()},
		{"iss": issuer, "sub": "x", "aud": resourceURL, "exp": time.Now().Add(-time.Hour).Unix()},
		{"iss": "wrong", "sub": "x", "aud": resourceURL, "exp": time.Now().Add(time.Hour).Unix()},
		{"iss": issuer, "sub": "x", "aud": resourceURL, "exp": time.Now().Add(time.Hour).Unix(), "groups": "engineering"},
	} {
		req, _ := http.NewRequest(http.MethodPost, resourceURL, strings.NewReader("{}"))
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", signedToken(t, key, invalid)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("invalid token status = %d", resp.StatusCode)
		}
	}
	var sawDeny bool
	for _, span := range exporter.GetSpans() {
		if span.Name != "skills/get" {
			continue
		}
		attrs := make(map[string]string)
		for _, attr := range span.Attributes {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}
		if attrs["user.id"] != issuer+"|user-1" || attrs["client.id"] != "pi" || attrs["skill.source.revision"] == "" {
			t.Errorf("missing validated identity or revision in span: %v", attrs)
		}
		if attrs["skill.access.outcome"] == "deny" {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Error("no denied skill lookup span")
	}
}
