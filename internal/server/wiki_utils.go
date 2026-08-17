package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

func isFetch(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "fetch"
}

func parseWikiForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(8 << 20)
	}
	return r.ParseForm()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func extractWikiSources(markdown string) []string {
	matches := regexp.MustCompile(`(?:wiki|content/notes)/[^\s\)\]`+"`"+`]+`).FindAllString(markdown, -1)
	seen := map[string]bool{}
	var sources []string
	for _, match := range matches {
		path := strings.Trim(match, "`.,;，。；：")
		if seen[path] {
			continue
		}
		seen[path] = true
		sources = append(sources, path)
	}
	sort.Strings(sources)
	return sources
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
