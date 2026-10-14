package catalog

func cloneSkillEntry(entry SkillEntry) SkillEntry {
	return SkillEntry{
		URI:         entry.URI,
		Frontmatter: cloneFrontmatter(entry.Frontmatter),
		Resources:   append([]ResourceManifestEntry(nil), entry.Resources...),
	}
}

func cloneFrontmatter(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneFrontmatter(value)
	case []any:
		cloned := make([]any, len(value))
		for i := range value {
			cloned[i] = cloneJSONValue(value[i])
		}
		return cloned
	default:
		return value
	}
}
