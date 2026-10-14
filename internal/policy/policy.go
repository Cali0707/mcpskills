package policy

import (
	"fmt"
	"os"
	"strings"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"sigs.k8s.io/yaml"
)

// Policy is an immutable, separately managed set of skill access requirements.
// Skills not mentioned in the file are available to every authenticated user.
type Policy struct {
	rules map[string]map[string]bool
}

type document struct {
	Skills map[string]struct {
		AnyGroup []string `json:"anyGroup"`
	} `json:"skills"`
}

func Load(file string, source catalog.Catalog) (*Policy, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read skill policy: %w", err)
	}
	var doc document
	if err := yaml.UnmarshalStrict(content, &doc); err != nil {
		return nil, fmt.Errorf("parse skill policy: %w", err)
	}
	entries, err := source.ListSkills()
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(entries))
	for _, entry := range entries {
		known[entry.URI] = true
	}
	p := &Policy{rules: make(map[string]map[string]bool, len(doc.Skills))}
	for uri, rule := range doc.Skills {
		if !known[uri] {
			return nil, fmt.Errorf("policy references unknown skill %q", uri)
		}
		if len(rule.AnyGroup) == 0 {
			return nil, fmt.Errorf("policy for %q needs at least one anyGroup", uri)
		}
		groups := make(map[string]bool, len(rule.AnyGroup))
		for _, group := range rule.AnyGroup {
			if strings.TrimSpace(group) != group || group == "" {
				return nil, fmt.Errorf("invalid group in policy for %q", uri)
			}
			groups[group] = true
		}
		p.rules[uri] = groups
	}
	return p, nil
}

// Allows applies any-of semantics to the validated principal's groups.
func (p *Policy) Allows(uri string, groups []string) bool {
	if p == nil {
		return false
	}
	required, restricted := p.rules[uri]
	if !restricted {
		return true
	}
	for _, group := range groups {
		if required[group] {
			return true
		}
	}
	return false
}
