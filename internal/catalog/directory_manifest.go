package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"sort"
	"strings"
)

func buildCatalogSnapshot(tree *sourceTree) (*catalogSnapshot, error) {
	snapshot := &catalogSnapshot{
		skillByURI:      make(map[string]SkillEntry),
		resources:       make(map[string][]byte),
		directories:     make(map[string][]DirectoryEntry),
		resourceOwners:  make(map[string]string),
		directoryOwners: make(map[string]string),
	}
	resourceEntries := make(map[string]ResourceManifestEntry)
	for _, relDir := range sortedSkillDirectories(tree.skills) {
		skill := tree.skills[relDir]
		manifest, err := indexSkill(snapshot, resourceEntries, skill, tree)
		if err != nil {
			return nil, err
		}
		entry := SkillEntry{
			URI:         skill.uri,
			Frontmatter: skill.frontmatter,
			Resources:   manifest,
		}
		snapshot.skillByURI[entry.URI] = entry
		snapshot.skills = append(snapshot.skills, entry)
	}
	sort.Slice(snapshot.skills, func(i, j int) bool { return snapshot.skills[i].URI < snapshot.skills[j].URI })
	for uri := range snapshot.directories {
		sort.Slice(snapshot.directories[uri], func(i, j int) bool {
			return snapshot.directories[uri][i].Name < snapshot.directories[uri][j].Name
		})
	}
	snapshot.revision = snapshotFingerprint(resourceEntries, snapshot.directories)
	return snapshot, nil
}

func sortedSkillDirectories(skills map[string]*discoveredSkill) []string {
	result := make([]string, 0, len(skills))
	for relDir := range skills {
		result = append(result, relDir)
	}
	sort.Strings(result)
	return result
}

func indexSkill(snapshot *catalogSnapshot, resources map[string]ResourceManifestEntry, skill *discoveredSkill, tree *sourceTree) ([]ResourceManifestEntry, error) {
	for relDir := range tree.directories {
		if !isWithin(skill.relDir, relDir) {
			continue
		}
		// A nested skill owns its own namespace. Its directory may still be a
		// child of this skill, but its contents must not enter this manifest.
		if owner := closestSkill(relDir, tree.skills); owner != skill && relDir != owner.relDir {
			continue
		}
		uri, err := directoryURIForPath(skill, relDir, tree.skills)
		if err != nil {
			return nil, err
		}
		if _, exists := snapshot.directories[uri]; !exists {
			snapshot.directories[uri] = nil
		}
		if relDir == skill.relDir {
			snapshot.directoryOwners[uri] = skill.uri
		} else if owner := closestSkill(relDir, tree.skills); owner == skill {
			snapshot.directoryOwners[uri] = skill.uri
		}
		if relDir == skill.relDir {
			continue
		}
		parentURI, err := directoryURIForPath(skill, path.Dir(relDir), tree.skills)
		if err != nil {
			return nil, err
		}
		name := path.Base(relDir)
		if nested := tree.skills[relDir]; nested != nil {
			name = nested.name
		}
		if err := addDirectoryChild(snapshot.directories, parentURI, DirectoryEntry{
			URI: uri, Name: name, MIMEType: directoryMIMEType,
		}); err != nil {
			return nil, err
		}
	}

	manifest := make([]ResourceManifestEntry, 0)
	for filePath, content := range tree.files {
		parent := path.Dir(filePath)
		if !isWithin(skill.relDir, parent) || closestSkill(parent, tree.skills) != skill {
			continue
		}
		uri, err := resourceURIForPath(skill, filePath, tree.skills)
		if err != nil {
			return nil, err
		}
		if previous, exists := snapshot.resources[uri]; exists {
			if !bytes.Equal(previous, content) {
				return nil, fmt.Errorf("resource URI collision at %q", uri)
			}
		} else {
			snapshot.resources[uri] = bytes.Clone(content)
		}
		snapshot.resourceOwners[uri] = skill.uri
		digest := sha256.Sum256(content)
		resource := ResourceManifestEntry{
			URI:    uri,
			Digest: sha256DigestPrefix + hex.EncodeToString(digest[:]),
			Size:   int64(len(content)),
		}
		resources[uri] = resource
		manifest = append(manifest, resource)

		parentURI, err := directoryURIForPath(skill, parent, tree.skills)
		if err != nil {
			return nil, err
		}
		name := path.Base(filePath)
		if err := addDirectoryChild(snapshot.directories, parentURI, DirectoryEntry{
			URI: uri, Name: name, MIMEType: resourceMIMEType(name),
		}); err != nil {
			return nil, err
		}
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].URI < manifest[j].URI })
	return manifest, nil
}

func closestSkill(relDir string, skills map[string]*discoveredSkill) *discoveredSkill {
	for {
		if skill := skills[relDir]; skill != nil {
			return skill
		}
		if relDir == "." {
			return nil
		}
		relDir = path.Dir(relDir)
	}
}

func resourceMIMEType(name string) string {
	if strings.EqualFold(path.Ext(name), ".md") {
		return markdownMIMEType
	}
	if mimeType := mime.TypeByExtension(path.Ext(name)); mimeType != "" {
		return mimeType
	}
	return unknownResourceMIMEType
}

func addDirectoryChild(target map[string][]DirectoryEntry, directoryURI string, child DirectoryEntry) error {
	for _, existing := range target[directoryURI] {
		if existing.Name == child.Name {
			if existing.URI == child.URI && existing.MIMEType == child.MIMEType {
				return nil
			}
			return fmt.Errorf("directory child collision at %q for %q", directoryURI, child.Name)
		}
	}
	target[directoryURI] = append(target[directoryURI], child)
	return nil
}

func snapshotFingerprint(resources map[string]ResourceManifestEntry, directories map[string][]DirectoryEntry) string {
	uris := make([]string, 0, len(resources))
	for uri := range resources {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	hash := sha256.New()
	for _, uri := range uris {
		resource := resources[uri]
		_, _ = fmt.Fprintf(hash, "file\x00%s\x00%s\x00%d\x00", uri, resource.Digest, resource.Size)
	}
	uris = uris[:0]
	for uri := range directories {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		_, _ = fmt.Fprintf(hash, "directory\x00%s\x00", uri)
		for _, child := range directories[uri] {
			_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", child.Name, child.URI, child.MIMEType)
		}
	}
	return sha256DigestPrefix + hex.EncodeToString(hash.Sum(nil))
}
