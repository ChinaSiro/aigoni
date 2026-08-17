package server

import (
	"path/filepath"
	"strings"

	"aigoni/internal/content"
)

func noteSourcePath(root string, note *content.Item) string {
	if note == nil {
		return ""
	}
	path := note.Path
	if rel, err := filepath.Rel(root, note.Path); err == nil && !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator)+"..") && !filepath.IsAbs(rel) {
		path = rel
	}
	return filepath.ToSlash(path)
}
