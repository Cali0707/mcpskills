package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	traversalResourceURI        = "skill://safe/../outside"
	escapedTraversalResourceURI = "skill://safe/%2e%2e/outside"
	encodedSlashResourceURI     = "skill://safe/%2Foutside"
	trailingSlashResourceURI    = "skill://safe/"
)

func TestDirectorySourceRejectsInvalidSkills(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "missing frontmatter", content: "# No metadata\n"},
		{name: "non-skill YAML end marker", content: "---\nname: wrong-delimiter\ndescription: required\n...\n"},
		{name: "missing name", content: "---\ndescription: required\n---\n"},
		{name: "missing description", content: "---\nname: missing-description\n---\n"},
		{name: "non-string name", content: "---\nname: 42\ndescription: required\n---\n"},
		{name: "invalid name segment", content: "---\nname: invalid_name\ndescription: required\n---\n"},
		{name: "invalid yaml", content: "---\nname: [\ndescription: broken\n---\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, skillFileName, test.content)
			if _, err := NewDirectorySource(root); err == nil {
				t.Fatal("NewDirectorySource accepted invalid SKILL.md frontmatter")
			}
		})
	}
}

func TestDirectorySourceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, skillFileName, "---\nname: safe\ndescription: Traversal checks.\n---\nbody\n")
	source, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("NewDirectorySource(): %v", err)
	}
	for _, uri := range []string{
		traversalResourceURI,
		escapedTraversalResourceURI,
		encodedSlashResourceURI,
		trailingSlashResourceURI,
	} {
		if _, err := source.ReadResource(uri); !errors.Is(err, ErrResourceNotFound) {
			t.Errorf("ReadResource(%q) error = %v, want ErrResourceNotFound", uri, err)
		}
		if _, err := source.ReadDirectory(uri); !errors.Is(err, ErrDirectoryNotFound) {
			t.Errorf("ReadDirectory(%q) error = %v, want ErrDirectoryNotFound", uri, err)
		}
	}
}

func TestDirectorySourceKeepsResourcesWithinTheRequestedSkillNamespace(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "alpha/SKILL.md", "---\nname: alpha\ndescription: First skill.\n---\n")
	writeFixture(t, root, "beta/SKILL.md", "---\nname: beta\ndescription: Second skill.\n---\n")
	writeFixture(t, root, "beta/private/notes.md", "belongs to beta")
	source, err := NewDirectorySource(root)
	if err != nil {
		t.Fatalf("NewDirectorySource(): %v", err)
	}

	if _, err := source.ReadResource("skill://alpha/private/notes.md"); !errors.Is(err, ErrResourceNotFound) {
		t.Errorf("cross-skill ReadResource error = %v, want ErrResourceNotFound", err)
	}
	if _, err := source.ReadDirectory("skill://alpha/private"); !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("cross-skill ReadDirectory error = %v, want ErrDirectoryNotFound", err)
	}
}

func TestDirectorySourceRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, skillFileName, "---\nname: safe\ndescription: Symlink checks.\n---\n")
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatalf("create fixture symlink: %v", err)
	}
	if _, err := NewDirectorySource(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("NewDirectorySource() error = %v, want symlink rejection", err)
	}
}

func TestDirectorySourceRejectsConfiguredRootSymlink(t *testing.T) {
	realRoot := t.TempDir()
	writeFixture(t, realRoot, skillFileName, "---\nname: root-link\ndescription: Root checks.\n---\n")
	link := filepath.Join(t.TempDir(), "skills")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	if _, err := NewDirectorySource(link); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Errorf("NewDirectorySource() error = %v, want root symlink rejection", err)
	}
}
