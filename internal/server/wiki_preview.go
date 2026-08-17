package server

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func splitWikiPreviewFrontMatter(markdown string) (string, map[string]any) {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return markdown, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(normalized, "---\n"), "\n---\n", 2)
	if len(parts) != 2 {
		return markdown, nil
	}
	meta := map[string]any{}
	if err := yaml.Unmarshal([]byte(parts[0]), &meta); err != nil {
		return markdown, nil
	}
	return strings.TrimPrefix(parts[1], "\n"), meta
}
