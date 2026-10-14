package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Cali0707/mcpskills/internal/catalog"
)

func TestLoadAndEvaluateSkillPolicy(t *testing.T) {
	source, err := catalog.NewDirectorySource(filepath.Join("..", "catalog", "testdata", "skills"))
	if err != nil { t.Fatal(err) }
	file := filepath.Join(t.TempDir(), "policy.yaml")
	for _, tc := range []struct { name, contents string; valid bool }{
		{"restricted", "skills:\n  'skill://toolbox/nested-tool/SKILL.md':\n    anyGroup: [engineering, sales]\n", true},
		{"unknown", "skills:\n  'skill://missing/SKILL.md':\n    anyGroup: [engineering]\n", false},
		{"empty-rule", "skills:\n  'skill://toolbox/SKILL.md':\n    anyGroup: []\n", false},
		{"typo", "skills:\n  'skill://toolbox/SKILL.md':\n    anyGroups: [engineering]\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(tc.contents), 0600); err != nil { t.Fatal(err) }
			p, err := Load(file, source)
			if (err == nil) != tc.valid { t.Fatalf("Load() = %v, valid = %v", err, tc.valid) }
			if !tc.valid { return }
			if p.Allows("skill://toolbox/nested-tool/SKILL.md", nil) || !p.Allows("skill://toolbox/nested-tool/SKILL.md", []string{"engineering"}) || !p.Allows("skill://toolbox/SKILL.md", nil) {
				t.Fatal("group or unrestricted access incorrect")
			}
		})
	}
}
