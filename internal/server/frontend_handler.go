package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"aigoni/internal/config"
	"aigoni/internal/content"
)

const defaultFrontendArticleImage = "/uploads/site/logo.png"

func (s *Server) webFrontend(w http.ResponseWriter, r *http.Request) {
	s.serveSPA(w, r, s.env.WebFrontendDir, s.webFrontendFS, "/_app/web/")
}

func (s *Server) adminFrontend(w http.ResponseWriter, r *http.Request) {
	s.serveAdminSPA(w, r, s.env.AdminFrontendDir, s.adminFrontendFS)
}

func (s *Server) serveAdminSPA(w http.ResponseWriter, r *http.Request, dir string, frontendFS fs.FS) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/_app/admin/") {
		s.serveSPA(w, r, dir, frontendFS, "/_app/admin/")
		return
	}
	data, err := s.readFrontendIndex(dir, frontendFS)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	meta := `<meta name="aigoni-admin-path" content="` + html.EscapeString(s.adminPath()) + `" />`
	data = bytes.Replace(data, []byte("</head>"), []byte("  "+meta+"\n  </head>"), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// serveSPA serves files only from its explicit namespace and otherwise uses index.html.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request, dir string, frontendFS fs.FS, namespace string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if requestPath, ok := strings.CutPrefix(r.URL.Path, namespace); ok {
		if s.serveFrontendFile(w, r, dir, frontendFS, requestPath) {
			return
		}
	}

	if slug, ok := frontendArticleSlug(r.URL.Path); ok {
		item, err := s.repo.GetBySlug(slug, content.TypePost)
		if err != nil || !item.Publish {
			http.NotFound(w, r)
			return
		}
		if err := s.serveFrontendArticle(w, dir, frontendFS, r.URL.Path, item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.serveFrontendIndex(w, r, dir, frontendFS)
}

func (s *Server) serveFrontendFile(w http.ResponseWriter, r *http.Request, dir string, frontendFS fs.FS, requestPath string) bool {
	if dir != "" {
		frontendDir := config.Abs(s.root, dir)
		filePath, ok := spaFilePath(frontendDir, requestPath)
		if !ok {
			http.NotFound(w, r)
			return true
		}
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			http.ServeFile(w, r, filePath)
			return true
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return true
		}
		return false
	}
	if frontendFS == nil {
		http.NotFound(w, r)
		return true
	}
	cleanPath, ok := spaFSPath(requestPath)
	if !ok {
		http.NotFound(w, r)
		return true
	}
	file, err := frontendFS.Open(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	if info.IsDir() {
		return false
	}
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "embedded frontend file is not seekable", http.StatusInternalServerError)
		return true
	}
	http.ServeContent(w, r, cleanPath, info.ModTime(), seeker)
	return true
}

func (s *Server) serveFrontendIndex(w http.ResponseWriter, r *http.Request, dir string, frontendFS fs.FS) {
	data, err := s.readFrontendIndex(dir, frontendFS)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) readFrontendIndex(dir string, frontendFS fs.FS) ([]byte, error) {
	if dir != "" {
		return os.ReadFile(filepath.Join(config.Abs(s.root, dir), "index.html"))
	}
	if frontendFS == nil {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(frontendFS, "index.html")
}

func spaFSPath(requestPath string) (string, bool) {
	cleanPath := path.Clean("/" + requestPath)
	if cleanPath == "/" || strings.HasPrefix(cleanPath, "/../") {
		return "", false
	}
	return strings.TrimPrefix(cleanPath, "/"), true
}

func spaFilePath(root, requestPath string) (string, bool) {
	cleanPath := path.Clean("/" + requestPath)
	if cleanPath == "/" || strings.HasPrefix(cleanPath, "/../") {
		return "", false
	}
	filePath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
	rel, err := filepath.Rel(root, filePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filePath, true
}

func frontendArticleSlug(path string) (string, bool) {
	const prefix = "/post/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	slug, err := url.PathUnescape(strings.TrimPrefix(path, prefix))
	if err != nil || slug == "" || strings.Contains(slug, "/") {
		return "", false
	}
	return slug, true
}

func (s *Server) serveFrontendArticle(w http.ResponseWriter, dir string, frontendFS fs.FS, requestPath string, item *content.Item) error {
	data, err := s.readFrontendIndex(dir, frontendFS)
	if err != nil {
		return err
	}

	description := frontendArticleDescription(item)
	pageURL := frontendAbsoluteURL(s.cfg.Site.BaseURL, requestPath)
	imageURL := frontendAbsoluteURL(s.cfg.Site.BaseURL, item.CoverImage)
	if item.CoverImage == "" {
		imageURL = frontendAbsoluteURL(s.cfg.Site.BaseURL, defaultFrontendArticleImage)
	}
	title := item.Title + "｜" + s.cfg.Site.Name
	structuredData, err := json.Marshal(map[string]any{
		"@context":         "https://schema.org",
		"@type":            "Article",
		"headline":         item.Title,
		"description":      description,
		"datePublished":    item.Date,
		"dateModified":     item.Lastmod,
		"mainEntityOfPage": map[string]string{"@type": "WebPage", "@id": pageURL},
		"image":            []string{imageURL},
		"articleSection":   item.Category,
		"inLanguage":       "zh-CN",
		"author":           map[string]string{"@type": "Person", "name": s.cfg.Site.Author},
		"publisher":        map[string]string{"@type": "Organization", "name": s.cfg.Site.Name, "url": strings.TrimRight(s.cfg.Site.BaseURL, "/")},
	})
	if err != nil {
		return err
	}

	head := strings.Join([]string{
		"<title>" + html.EscapeString(title) + "</title>",
		frontendMeta("name", "description", description),
		frontendMeta("name", "robots", "index, follow"),
		`<link rel="canonical" href="` + html.EscapeString(pageURL) + `" />`,
		frontendMeta("property", "og:type", "article"),
		frontendMeta("property", "og:title", title),
		frontendMeta("property", "og:description", description),
		frontendMeta("property", "og:url", pageURL),
		frontendMeta("property", "og:image", imageURL),
		frontendMeta("property", "og:image:alt", item.Title),
		frontendMeta("property", "article:published_time", item.Date.Format("2006-01-02T15:04:05Z07:00")),
		frontendMeta("property", "article:modified_time", item.Lastmod.Format("2006-01-02T15:04:05Z07:00")),
		frontendMeta("property", "article:section", item.Category),
		frontendMeta("name", "twitter:card", "summary_large_image"),
		frontendMeta("name", "twitter:title", title),
		frontendMeta("name", "twitter:description", description),
		frontendMeta("name", "twitter:image", imageURL),
		`<script id="article-structured-data" type="application/ld+json">` + string(structuredData) + `</script>`,
	}, "\n      ")

	output := replaceFrontendHead(data, head)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(output)
	return err
}

func frontendArticleDescription(item *content.Item) string {
	if description := strings.TrimSpace(item.Description); description != "" {
		return description
	}

	text := strings.NewReplacer(
		"#", " ",
		"*", " ",
		"_", " ",
		"`", " ",
		">", " ",
		"[", " ",
		"]", " ",
		"(", " ",
		")", " ",
		"!", " ",
	).Replace(item.Body)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 160 {
		runes = runes[:160]
	}
	return string(runes)
}

func frontendAbsoluteURL(baseURL, value string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost"
	}
	if value == "" {
		return baseURL + "/"
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	return baseURL + "/" + strings.TrimLeft(value, "/")
}

func frontendMeta(attribute, key, value string) string {
	return `<meta ` + attribute + `="` + html.EscapeString(key) + `" content="` + html.EscapeString(value) + `" />`
}

func replaceFrontendHead(data []byte, head string) []byte {
	const placeholder = "<!-- aigoni:head -->"
	if bytes.Contains(data, []byte(placeholder)) {
		return bytes.Replace(data, []byte(placeholder), []byte(head), 1)
	}
	// Older builds without the marker retain their original head and receive appended metadata.
	return bytes.Replace(data, []byte("</head>"), []byte(head+"\n    </head>"), 1)
}
