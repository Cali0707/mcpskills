package catalog

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type sourceTree struct {
	files       map[string][]byte
	directories map[string]struct{}
	skills      map[string]*discoveredSkill
}

func loadDirectorySnapshot(root string) (*catalogSnapshot, error) {
	tree, err := scanDirectoryRoot(root)
	if err != nil {
		return nil, err
	}
	if err := assignSkillURIs(tree.skills); err != nil {
		return nil, err
	}
	return buildCatalogSnapshot(tree)
}

func scanDirectoryRoot(root string) (*sourceTree, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("skills root must not be empty")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve skills root: %w", err)
	}
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("stat skills root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("skills root %q must be a real directory", root)
	}

	tree := &sourceTree{
		files:       make(map[string][]byte),
		directories: map[string]struct{}{".": {}},
		skills:      make(map[string]*discoveredSkill),
	}
	err = filepath.WalkDir(absoluteRoot, func(fullPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fullPath == absoluteRoot {
			return nil
		}
		rel, err := filepath.Rel(absoluteRoot, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %q is not allowed in skills root", rel)
		}
		if entry.IsDir() {
			if path.Base(rel) == gitDirectoryName {
				return filepath.SkipDir
			}
			tree.directories[rel] = struct{}{}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file %q is not allowed in skills root", rel)
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read %q: %w", rel, err)
		}
		tree.files[rel] = content
		if path.Base(rel) == skillFileName {
			if err := tree.addSkill(path.Dir(rel), rel, content); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills root: %w", err)
	}
	return tree, nil
}

func (tree *sourceTree) addSkill(relDir, filePath string, content []byte) error {
	frontmatter, err := parseFrontmatter(content)
	if err != nil {
		return fmt.Errorf("parse %q frontmatter: %w", filePath, err)
	}
	name, ok := frontmatter[frontmatterNameField].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%q frontmatter requires a non-empty string name", filePath)
	}
	if len(name) > maxSkillNameLength || !skillNamePattern.MatchString(name) {
		return fmt.Errorf("%q frontmatter name %q is not a valid skill URI segment", filePath, name)
	}
	description, ok := frontmatter[frontmatterDescriptionField].(string)
	if !ok || strings.TrimSpace(description) == "" {
		return fmt.Errorf("%q frontmatter requires a non-empty string description", filePath)
	}
	tree.skills[relDir] = &discoveredSkill{relDir: relDir, name: name, frontmatter: frontmatter}
	return nil
}
