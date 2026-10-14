package config

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
)

const (
	defaultListenAddr   = "127.0.0.1:8080"
	defaultEndpointPath = "/mcp"

	listenAddrEnv   = "MCP_LISTEN_ADDR"
	endpointPathEnv = "MCP_ENDPOINT_PATH"
	skillsDirEnv    = "MCP_SKILLS_DIR"
	policyFileEnv   = "MCP_POLICY_FILE"
	issuerEnv       = "MCP_OAUTH_ISSUER"
	resourceEnv     = "MCP_RESOURCE_URL"
	groupsClaimEnv  = "MCP_GROUPS_CLAIM"
)

// Config contains the settings needed to expose the MCP endpoint.
type Config struct {
	ListenAddr   string
	EndpointPath string
	SkillsDir    string
	PolicyFile   string
	Issuer       string
	ResourceURL  string
	GroupsClaim  string
}

// LoadConfig reads server settings from the environment, applying local-development defaults.
func LoadConfig() (Config, error) {
	config := Config{
		ListenAddr:   valueOrDefault(listenAddrEnv, defaultListenAddr),
		EndpointPath: valueOrDefault(endpointPathEnv, defaultEndpointPath),
		SkillsDir:    os.Getenv(skillsDirEnv),
		PolicyFile:  os.Getenv(policyFileEnv),
		Issuer:      valueOrDefault(issuerEnv, "http://localhost:8180/realms/mcpskills-local"),
		ResourceURL: valueOrDefault(resourceEnv, "http://localhost:8080/mcp"),
		GroupsClaim: valueOrDefault(groupsClaimEnv, "groups"),
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func valueOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.SkillsDir) == "" {
		return fmt.Errorf("%s must be set to a skills directory", skillsDirEnv)
	}
	if strings.TrimSpace(c.PolicyFile) == "" {
		return fmt.Errorf("%s must be set to a policy file", policyFileEnv)
	}
	if c.GroupsClaim == "" || strings.ContainsAny(c.GroupsClaim, " .") {
		return fmt.Errorf("%s must be a top-level token claim name", groupsClaimEnv)
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("%s must not be empty", listenAddrEnv)
	}
	if !strings.HasPrefix(c.EndpointPath, "/") || c.EndpointPath == "/" || strings.ContainsAny(c.EndpointPath, "?#{}*") {
		return fmt.Errorf("%s must be an absolute non-root path without query, fragment, or ServeMux wildcard syntax", endpointPathEnv)
	}
	if strings.HasSuffix(c.EndpointPath, "/") || path.Clean(c.EndpointPath) != c.EndpointPath {
		return fmt.Errorf("%s must be a clean exact path without a trailing slash", endpointPathEnv)
	}
	for key, value := range map[string]string{issuerEnv: c.Issuer, resourceEnv: c.ResourceURL} {
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s must be an absolute HTTPS URL (HTTP allowed on localhost)", key)
		}
	}
	u, _ := url.Parse(c.ResourceURL)
	if u.Path != c.EndpointPath {
		return fmt.Errorf("%s path must match %s", resourceEnv, endpointPathEnv)
	}
	return nil
}
