package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectorySourceReturnsDefensiveCopies(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, skillFileName, "---\nname: immutable\ndescription: Copy checks.\nmetadata:\n  owner: original\n---\nbody\n")
	source, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("NewDirectorySource(): %v", err)
	}

	entry := mustGetSkill(t, source, "skill://immutable/SKILL.md")
	entry.Frontmatter["metadata"].(map[string]any)["owner"] = "mutated"
	entry.Resources[0].URI = "skill://wrong/SKILL.md"

	listed, err := source.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	listed[0].Frontmatter["name"] = "changed"
	listed[0].Resources[0].Digest = "changed"

	served, err := source.ReadResource("skill://immutable/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	served[0] = '!'

	unchanged := mustGetSkill(t, source, "skill://immutable/SKILL.md")
	if unchanged.Frontmatter["name"] != "immutable" || unchanged.Frontmatter["metadata"].(map[string]any)["owner"] != "original" {
		t.Errorf("returned frontmatter mutated snapshot state: %#v", unchanged.Frontmatter)
	}
	if unchanged.Resources[0].URI != "skill://immutable/SKILL.md" {
		t.Errorf("returned manifest mutated snapshot state: %+v", unchanged.Resources)
	}
	originalFile, err := os.ReadFile(filepath.Join(root, skillFileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(served, mustReadResource(t, source, unchanged.URI)) {
		t.Error("ReadResource returned the snapshot's mutable byte slice")
	}
	if !bytes.Equal(originalFile, mustReadResource(t, source, unchanged.URI)) {
		t.Error("mutating the returned bytes changed the snapshot resource")
	}
}
