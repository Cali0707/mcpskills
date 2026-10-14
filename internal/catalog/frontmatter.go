package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

func parseFrontmatter(content []byte) (map[string]any, error) {
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 || strings.TrimSuffix(string(lines[0]), "\r") != frontmatterDelimiter {
		return nil, errors.New("SKILL.md must begin with YAML frontmatter")
	}
	closingLine := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(string(lines[i]), "\r")
		if line == frontmatterDelimiter {
			closingLine = i
			break
		}
	}
	if closingLine == -1 {
		return nil, errors.New("YAML frontmatter is missing its closing delimiter")
	}

	var fields map[string]any
	if err := yaml.Unmarshal(bytes.Join(lines[1:closingLine], []byte("\n")), &fields); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}
	if fields == nil {
		return nil, errors.New("YAML frontmatter must be a mapping")
	}
	return fields, nil
}
