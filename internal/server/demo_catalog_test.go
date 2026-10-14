package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/Cali0707/mcpskills/internal/policy"
)

func TestDemoCatalogMatchesLocalKeycloakGroups(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "examples")
	source, err := catalog.NewDirectorySource(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Load(filepath.Join(root, "skill-policy.yaml"), source)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join("..", "..", "local", "keycloak", "mcpskills-local-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm struct {
		Users []struct {
			Username string   `json:"username"`
			Groups   []string `json:"groups"`
		} `json:"users"`
	}
	if err := json.Unmarshal(contents, &realm); err != nil {
		t.Fatal(err)
	}
	entries, err := source.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"alice": {"skill://incident-triage/SKILL.md", "skill://launch-brief/SKILL.md", "skill://launch-brief/engineering-rollout/SKILL.md"},
		"bob":   {"skill://customer-update/SKILL.md", "skill://launch-brief/SKILL.md"},
		"carol": {"skill://launch-brief/SKILL.md"},
	}
	for _, user := range realm.Users {
		groups := make([]string, len(user.Groups))
		for i, group := range user.Groups {
			groups[i] = strings.TrimPrefix(group, "/")
		}
		var visible []string
		for _, entry := range entries {
			if rules.Allows(entry.URI, groups) {
				visible = append(visible, entry.URI)
			}
		}
		sort.Strings(visible)
		expected := append([]string(nil), want[user.Username]...)
		sort.Strings(expected)
		if !reflect.DeepEqual(visible, expected) {
			t.Errorf("%s sees %v, want %v", user.Username, visible, expected)
		}
		delete(want, user.Username)
	}
	if len(want) != 0 {
		t.Errorf("demo accounts missing from local realm: %v", want)
	}

	parent, err := source.GetSkill("skill://launch-brief/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range parent.Resources {
		if strings.Contains(file.URI, "/engineering-rollout/") {
			t.Errorf("restricted nested file in public parent manifest: %s", file.URI)
		}
	}
}

func TestDemoSkillsListWithNewProtocolRequest(t *testing.T) {
	source, err := catalog.NewDirectorySource(filepath.Join("..", "..", "docs", "examples", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMCPServer(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"skills/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"demo-curl","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(request))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	r.Header.Set("Mcp-Method", "skills/list")
	w := httptest.NewRecorder()
	newHTTPHandler(server, "/mcp", nil).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("skills/list: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Result struct {
			Skills []catalog.SkillEntry `json:"skills"`
		} `json:"result"`
	}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &response); err != nil {
			t.Fatal(err)
		}
	}
	if len(response.Result.Skills) != 4 {
		t.Fatalf("new-protocol skills/list: %d skills; response: %s", len(response.Result.Skills), w.Body.String())
	}
}
