package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func assignSkillURIs(skills map[string]*discoveredSkill) error {
	rootSkill := skills["."]
	for relDir, skill := range skills {
		segments := skillDirectorySegments(relDir, skill, rootSkill, skills)
		uri, err := uriFromSegments(segments)
		if err != nil {
			return fmt.Errorf("build URI for skill directory %q: %w", relDir, err)
		}
		if segments[len(segments)-1] != skill.name {
			return fmt.Errorf("skill %q URI final path segment %q does not match frontmatter name %q", relDir, segments[len(segments)-1], skill.name)
		}
		skill.uriSegments = segments
		skill.uri = uri + "/" + skillFileName
	}
	if err := validateUniqueSkillURIs(skills); err != nil {
		return err
	}
	return nil
}

func skillDirectorySegments(relDir string, skill, rootSkill *discoveredSkill, skills map[string]*discoveredSkill) []string {
	if relDir == "." {
		return []string{skill.name}
	}
	segments := make([]string, 0, strings.Count(relDir, "/")+2)
	if rootSkill != nil {
		segments = append(segments, rootSkill.name)
	}
	prefix := ""
	for _, part := range strings.Split(relDir, "/") {
		prefix = path.Join(prefix, part)
		if ancestor := skills[prefix]; ancestor != nil {
			part = ancestor.name
		}
		segments = append(segments, part)
	}
	return segments
}

func validateUniqueSkillURIs(skills map[string]*discoveredSkill) error {
	seen := make(map[string]string, len(skills))
	for relDir, skill := range skills {
		if previous, exists := seen[skill.uri]; exists {
			return fmt.Errorf("skill URI collision: %q and %q both map to %q", previous, relDir, skill.uri)
		}
		seen[skill.uri] = relDir
	}
	return nil
}

func resourceURIForPath(skill *discoveredSkill, fileRel string, skills map[string]*discoveredSkill) (string, error) {
	if fileRel == skill.relDir || !isWithin(skill.relDir, path.Dir(fileRel)) {
		return "", fmt.Errorf("resource %q is outside skill root %q", fileRel, skill.relDir)
	}
	segments, err := directorySegments(skill, path.Dir(fileRel), skills)
	if err != nil {
		return "", err
	}
	segments = append(segments, path.Base(fileRel))
	return uriFromSegments(segments)
}

func directoryURIForPath(skill *discoveredSkill, relDir string, skills map[string]*discoveredSkill) (string, error) {
	segments, err := directorySegments(skill, relDir, skills)
	if err != nil {
		return "", err
	}
	return uriFromSegments(segments)
}

// directorySegments replaces the filesystem names of nested skills with their
// frontmatter names, using the same mapping for directory and file URIs.
func directorySegments(skill *discoveredSkill, relDir string, skills map[string]*discoveredSkill) ([]string, error) {
	segments := append([]string(nil), skill.uriSegments...)
	if relDir == skill.relDir {
		return segments, nil
	}
	if !isWithin(skill.relDir, relDir) {
		return nil, fmt.Errorf("directory %q is outside skill root %q", relDir, skill.relDir)
	}
	relFromSkill := relDir
	if skill.relDir != "." {
		relFromSkill = strings.TrimPrefix(relDir, skill.relDir+"/")
	}
	globalDir := skill.relDir
	for _, part := range strings.Split(relFromSkill, "/") {
		globalDir = path.Join(globalDir, part)
		if nested := skills[globalDir]; nested != nil {
			part = nested.name
		}
		segments = append(segments, part)
	}
	return segments, nil
}

func uriFromSegments(segments []string) (string, error) {
	if len(segments) == 0 || !skillNamePattern.MatchString(segments[0]) {
		return "", fmt.Errorf("invalid skill URI authority %q", strings.Join(segments, "/"))
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\") {
			return "", fmt.Errorf("invalid skill URI path segment %q", segment)
		}
	}
	u := &url.URL{Scheme: skillURIScheme, Host: segments[0]}
	if len(segments) > 1 {
		u.Path = "/" + strings.Join(segments[1:], "/")
	}
	return u.String(), nil
}

func validateSkillURI(uri string) error {
	if err := validateResourceURI(uri); err != nil {
		return err
	}
	if !strings.HasSuffix(uri, "/"+skillFileName) {
		return errors.New("URI is not a SKILL.md URI")
	}
	return nil
}

func validateResourceURI(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	if u.Scheme != skillURIScheme || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("URI must be an absolute skill URI without query or fragment")
	}
	if strings.HasSuffix(u.Path, "/") {
		return errors.New("URI must not have a trailing slash")
	}
	if u.Path != "" {
		for _, escaped := range strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/") {
			segment, err := url.PathUnescape(escaped)
			if err != nil || segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\") {
				return errors.New("URI contains an invalid path segment")
			}
		}
	}
	if u.String() != uri {
		return errors.New("URI is not in canonical form")
	}
	return nil
}

func isWithin(root, candidate string) bool {
	if root == "." {
		return true
	}
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}
