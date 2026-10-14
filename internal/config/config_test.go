package config

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv(skillsDirEnv, "/tmp/skills")
	t.Setenv(policyFileEnv, "/tmp/policy.yaml")

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", got.ListenAddr, defaultListenAddr)
	}
	if got.EndpointPath != defaultEndpointPath {
		t.Errorf("EndpointPath = %q, want %q", got.EndpointPath, defaultEndpointPath)
	}
	if got.SkillsDir != "/tmp/skills" {
		t.Errorf("SkillsDir = %q, want /tmp/skills", got.SkillsDir)
	}
}

func TestLoadConfigEnvironmentOverrides(t *testing.T) {
	t.Setenv(listenAddrEnv, "0.0.0.0:9000")
	t.Setenv(endpointPathEnv, "/custom/mcp")
	t.Setenv(skillsDirEnv, "/custom/skills")
	t.Setenv(policyFileEnv, "/custom/policy.yaml")
	t.Setenv(resourceEnv, "http://localhost:8080/custom/mcp")

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got.ListenAddr != "0.0.0.0:9000" {
		t.Errorf("ListenAddr = %q, want %q", got.ListenAddr, "0.0.0.0:9000")
	}
	if got.EndpointPath != "/custom/mcp" {
		t.Errorf("EndpointPath = %q, want %q", got.EndpointPath, "/custom/mcp")
	}
	if got.SkillsDir != "/custom/skills" {
		t.Errorf("SkillsDir = %q, want /custom/skills", got.SkillsDir)
	}
}

func TestLoadConfigRejectsInvalidEndpointPath(t *testing.T) {
	t.Setenv(skillsDirEnv, "/tmp/skills")
	t.Setenv(policyFileEnv, "/tmp/policy.yaml")
	for _, endpointPath := range []string{
		"mcp",
		"/",
		"/mcp/",
		"/mcp/*",
		"/mcp/{$}",
		"/mcp/{path}",
		"/mcp/{rest...}",
	} {
		t.Run(endpointPath, func(t *testing.T) {
			t.Setenv(endpointPathEnv, endpointPath)
			t.Setenv(resourceEnv, "http://localhost:8080"+endpointPath)

			if _, err := LoadConfig(); err == nil {
				t.Fatal("LoadConfig() error = nil, want invalid endpoint path error")
			}
		})
	}
}

func TestLoadConfigRequiresSkillsDirectory(t *testing.T) {
	clearConfigEnvironment(t)
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() accepted an unset skills directory")
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{listenAddrEnv, endpointPathEnv, skillsDirEnv, policyFileEnv, issuerEnv, resourceEnv, groupsClaimEnv} {
		value, hadValue := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q): %v", key, err)
		}
		t.Cleanup(func() {
			if hadValue {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}
