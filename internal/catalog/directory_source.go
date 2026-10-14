package catalog

import "bytes"

// DirectorySource loads a directory tree once and serves an immutable snapshot.
type DirectorySource struct {
	snapshot *catalogSnapshot
}

var _ Catalog = (*DirectorySource)(nil)

type catalogSnapshot struct {
	skills          []SkillEntry
	skillByURI      map[string]SkillEntry
	resources       map[string][]byte
	directories     map[string][]DirectoryEntry
	resourceOwners  map[string]string
	directoryOwners map[string]string
	revision        string
}

type discoveredSkill struct {
	relDir      string
	name        string
	uri         string
	uriSegments []string
	frontmatter map[string]any
}

// NewDirectorySource loads root into a validated, immutable catalog snapshot.
func NewDirectorySource(root string) (*DirectorySource, error) {
	snapshot, err := loadDirectorySnapshot(root)
	if err != nil {
		return nil, err
	}
	return &DirectorySource{snapshot: snapshot}, nil
}

// ListSkills returns complete skills in deterministic URI order.
func (s *DirectorySource) ListSkills() ([]SkillEntry, error) {
	if s == nil || s.snapshot == nil {
		return nil, nil
	}
	result := make([]SkillEntry, len(s.snapshot.skills))
	for i, entry := range s.snapshot.skills {
		result[i] = cloneSkillEntry(entry)
	}
	return result, nil
}

// GetSkill returns a copy of the skill entry identified by its SKILL.md URI.
func (s *DirectorySource) GetSkill(uri string) (SkillEntry, error) {
	if s == nil || s.snapshot == nil || validateSkillURI(uri) != nil {
		return SkillEntry{}, ErrSkillNotFound
	}
	entry, ok := s.snapshot.skillByURI[uri]
	if !ok {
		return SkillEntry{}, ErrSkillNotFound
	}
	return cloneSkillEntry(entry), nil
}

// ReadResource returns a copy of the bytes retained by the snapshot.
func (s *DirectorySource) ReadResource(uri string) ([]byte, error) {
	if s == nil || s.snapshot == nil || validateResourceURI(uri) != nil {
		return nil, ErrResourceNotFound
	}
	content, ok := s.snapshot.resources[uri]
	if !ok {
		return nil, ErrResourceNotFound
	}
	return bytes.Clone(content), nil
}

// ReadDirectory returns a copy of the direct children of a snapshot directory.
func (s *DirectorySource) ReadDirectory(uri string) ([]DirectoryEntry, error) {
	if s == nil || s.snapshot == nil || validateResourceURI(uri) != nil {
		return nil, ErrDirectoryNotFound
	}
	children, ok := s.snapshot.directories[uri]
	if !ok {
		return nil, ErrDirectoryNotFound
	}
	return append([]DirectoryEntry(nil), children...), nil
}

// Revision returns the deterministic SHA-256 fingerprint of this snapshot.
func (s *DirectorySource) Revision() string {
	if s == nil || s.snapshot == nil {
		return ""
	}
	return s.snapshot.revision
}

// ResourceSkill returns the skill that owns this resource in the snapshot.
func (s *DirectorySource) ResourceSkill(uri string) (string, error) {
	if s == nil || s.snapshot == nil || validateResourceURI(uri) != nil {
		return "", ErrResourceNotFound
	}
	owner, ok := s.snapshot.resourceOwners[uri]
	if !ok {
		return "", ErrResourceNotFound
	}
	return owner, nil
}

// DirectorySkill returns the skill that owns this directory in the snapshot.
func (s *DirectorySource) DirectorySkill(uri string) (string, error) {
	if s == nil || s.snapshot == nil || validateResourceURI(uri) != nil {
		return "", ErrDirectoryNotFound
	}
	owner, ok := s.snapshot.directoryOwners[uri]
	if !ok {
		return "", ErrDirectoryNotFound
	}
	return owner, nil
}
