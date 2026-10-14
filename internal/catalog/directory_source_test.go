package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	parentSkillURI       = "skill://toolbox/SKILL.md"
	nestedSkillURI       = "skill://toolbox/nested-tool/SKILL.md"
	rootDirectoryURI     = "skill://toolbox"
	nestedDirectoryURI   = "skill://toolbox/nested-tool"
	referencesDirectory  = "skill://toolbox/references"
	guideResourceURI     = "skill://toolbox/references/guide.md"
	nestedSampleResource = "skill://toolbox/nested-tool/examples/sample.txt"
)

func TestDirectorySourceDiscoversNestedSkillsAndBuildsManifests(t *testing.T) {
	root := t.TempDir()
	copySkillTestdata(t, root)
	writeFixture(t, root, filepath.Join(gitDirectoryName, "hidden", skillFileName), "ignored repository data")

	source, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("NewDirectorySource() error = %v", err)
	}

	entries, err := source.ListSkills()
	if err != nil {
		t.Fatalf("ListSkills(): %v", err)
	}
	if got, want := skillURIs(entries), []string{parentSkillURI, nestedSkillURI}; !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered skill URIs = %v, want %v", got, want)
	}

	parent := mustGetSkill(t, source, parentSkillURI)
	assertFrontmatter(t, parent, map[string]any{
		"name":          "toolbox",
		"description":   "Parent skill.",
		"license":       "MIT",
		"compatibility": "Local tool environment.",
		"metadata": map[string]any{
			"owner": "tools",
			"tags":  "review, build",
		},
		"allowed-tools": "Read Grep",
		"team":          "platform",
	})
	parentFiles := map[string]string{
		parentSkillURI:   filepath.Join(root, skillFileName),
		guideResourceURI: filepath.Join(root, "references", "guide.md"),
	}
	assertManifestMatchesFiles(t, source, parent, parentFiles)

	nested := mustGetSkill(t, source, nestedSkillURI)
	assertManifestMatchesFiles(t, source, nested, map[string]string{
		nestedSkillURI:       filepath.Join(root, "nested", skillFileName),
		nestedSampleResource: filepath.Join(root, "nested", "examples", "sample.txt"),
	})
	if owner, err := source.ResourceSkill(nestedSampleResource); err != nil || owner != nestedSkillURI {
		t.Errorf("nested file owner = %q, %v", owner, err)
	}
	if owner, err := source.DirectorySkill(nestedDirectoryURI); err != nil || owner != nestedSkillURI {
		t.Errorf("nested directory owner = %q, %v", owner, err)
	}

	assertDirectoryChildren(t, source, rootDirectoryURI, []DirectoryEntry{
		{URI: parentSkillURI, Name: skillFileName, MIMEType: markdownMIMEType},
		{URI: nestedDirectoryURI, Name: "nested-tool", MIMEType: directoryMIMEType},
		{URI: referencesDirectory, Name: "references", MIMEType: directoryMIMEType},
	})
	assertDirectoryChildren(t, source, referencesDirectory, []DirectoryEntry{
		{URI: guideResourceURI, Name: "guide.md", MIMEType: markdownMIMEType},
	})

	second, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("reload unchanged source: %v", err)
	}
	if source.Revision() == "" || source.Revision() != second.Revision() {
		t.Errorf("unchanged source revisions differ: %q and %q", source.Revision(), second.Revision())
	}
	originalGuide := mustReadFile(t, parentFiles[guideResourceURI])
	writeFixture(t, root, "references/guide.md", "changed bytes\n")
	changed, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("reload changed source: %v", err)
	}
	if source.Revision() == changed.Revision() {
		t.Errorf("revision did not change when a resource changed: %q", source.Revision())
	}
	if got := mustReadResource(t, source, guideResourceURI); !bytes.Equal(got, originalGuide) {
		t.Errorf("existing snapshot returned changed file bytes: %q", got)
	}
}

func TestDirectorySourceRevisionTracksEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, skillFileName, "---\nname: toolbox\ndescription: A skill.\n---\n")
	before, err := NewDirectorySource(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(root, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := NewDirectorySource(root)
	if err != nil {
		t.Fatal(err)
	}
	assertDirectoryChildren(t, after, rootDirectoryURI, []DirectoryEntry{
		{URI: parentSkillURI, Name: skillFileName, MIMEType: markdownMIMEType},
		{URI: "skill://toolbox/empty", Name: "empty", MIMEType: directoryMIMEType},
	})
	if _, err := after.ReadDirectory("skill://toolbox/empty"); err != nil {
		t.Fatalf("ReadDirectory(empty): %v", err)
	}
	if before.Revision() == after.Revision() {
		t.Errorf("revision did not change when directory listing changed: %q", before.Revision())
	}
}

func writeFixture(t *testing.T, root, relativePath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", fullPath, err)
	}
}

func copySkillTestdata(t *testing.T, root string) {
	t.Helper()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "skills"))); err != nil {
		t.Fatalf("copy skill testdata: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return content
}

func skillURIs(entries []SkillEntry) []string {
	result := make([]string, len(entries))
	for i, entry := range entries {
		result[i] = entry.URI
	}
	return result
}

func mustGetSkill(t *testing.T, source *DirectorySource, uri string) SkillEntry {
	t.Helper()
	entry, err := source.GetSkill(uri)
	if err != nil {
		t.Fatalf("GetSkill(%q): %v", uri, err)
	}
	return entry
}

func assertFrontmatter(t *testing.T, entry SkillEntry, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(entry.Frontmatter, want) {
		t.Errorf("frontmatter = %#v, want %#v", entry.Frontmatter, want)
	}
}

func assertManifestMatchesFiles(t *testing.T, source *DirectorySource, entry SkillEntry, wantFiles map[string]string) {
	t.Helper()
	if len(entry.Resources) != len(wantFiles) {
		t.Fatalf("manifest for %q has %d resources, want %d", entry.URI, len(entry.Resources), len(wantFiles))
	}
	for _, resource := range entry.Resources {
		fixturePath, found := wantFiles[resource.URI]
		if !found {
			t.Errorf("manifest for %q contains unexpected URI %q", entry.URI, resource.URI)
			continue
		}
		content := mustReadFile(t, fixturePath)
		if resource.Size != int64(len(content)) {
			t.Errorf("resource %q size = %d, want %d", resource.URI, resource.Size, len(content))
		}
		digest := sha256.Sum256(content)
		wantDigest := sha256DigestPrefix + hex.EncodeToString(digest[:])
		if resource.Digest != wantDigest {
			t.Errorf("resource %q digest = %q, want %q", resource.URI, resource.Digest, wantDigest)
		}
		served, err := source.ReadResource(resource.URI)
		if err != nil {
			t.Errorf("ReadResource(%q): %v", resource.URI, err)
			continue
		}
		if !bytes.Equal(served, content) {
			t.Errorf("ReadResource(%q) returned bytes different from its manifest fixture", resource.URI)
		}
	}
}

func assertDirectoryChildren(t *testing.T, source *DirectorySource, uri string, want []DirectoryEntry) {
	t.Helper()
	children, err := source.ReadDirectory(uri)
	if err != nil {
		t.Fatalf("ReadDirectory(%q): %v", uri, err)
	}
	if !reflect.DeepEqual(children, want) {
		t.Errorf("ReadDirectory(%q) = %+v, want %+v", uri, children, want)
	}
}

func mustReadResource(t *testing.T, source *DirectorySource, uri string) []byte {
	t.Helper()
	content, err := source.ReadResource(uri)
	if err != nil {
		t.Fatalf("ReadResource(%q): %v", uri, err)
	}
	return content
}
