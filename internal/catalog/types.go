package catalog

import "errors"

const (
	skillURIScheme              = "skill"
	skillFileName               = "SKILL.md"
	frontmatterDelimiter        = "---"
	frontmatterNameField        = "name"
	frontmatterDescriptionField = "description"
	gitDirectoryName            = ".git"
	sha256DigestPrefix          = "sha256:"
	directoryMIMEType           = "inode/directory"
	markdownMIMEType            = "text/markdown"
	unknownResourceMIMEType     = "application/octet-stream"
	maxSkillNameLength          = 64
)

var (
	ErrSkillNotFound     = errors.New("skill not found")
	ErrResourceNotFound  = errors.New("resource not found")
	ErrDirectoryNotFound = errors.New("directory not found")
)

// ResourceManifestEntry describes the exact bytes associated with a resource URI.
type ResourceManifestEntry struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// SkillEntry is the complete, backend-neutral representation of a skill.
type SkillEntry struct {
	URI         string                  `json:"uri"`
	Frontmatter map[string]any          `json:"frontmatter"`
	Resources   []ResourceManifestEntry `json:"resources"`
}

// DirectoryEntry describes one direct child in a resource directory.
type DirectoryEntry struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
}

// SkillLister lists complete skill entries.
type SkillLister interface {
	ListSkills() ([]SkillEntry, error)
}

// SkillGetter looks up a complete skill entry by its SKILL.md URI.
type SkillGetter interface {
	GetSkill(uri string) (SkillEntry, error)
}

// ResourceReader reads the snapshot bytes for a resource URI.
type ResourceReader interface {
	ReadResource(uri string) ([]byte, error)
}

// DirectoryReader lists the direct children of a resource directory URI.
type DirectoryReader interface {
	ReadDirectory(uri string) ([]DirectoryEntry, error)
}

// Revisioner identifies the immutable catalog snapshot.
type Revisioner interface {
	Revision() string
}

// ResourceOwner resolves a resource to the single skill whose policy governs it.
type ResourceOwner interface {
	ResourceSkill(uri string) (string, error)
	DirectorySkill(uri string) (string, error)
}

// Catalog is the backend-neutral read boundary for a complete skills snapshot.
type Catalog interface {
	SkillLister
	SkillGetter
	ResourceReader
	DirectoryReader
	Revisioner
	ResourceOwner
}
