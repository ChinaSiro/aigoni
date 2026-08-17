package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"aigoni/internal/config"
)

// serveStaticFile 提供单个静态文件：目录请求返回 404，不做目录列表；
// 统一加 nosniff，避免浏览器内容嗅探带来的历史兼容风险。
func serveStaticFile(w http.ResponseWriter, r *http.Request, full string) {
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, full)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".md") || strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(config.Abs(s.root, s.cfg.Paths.UploadsDir), filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/uploads/")))
	serveStaticFile(w, r, full)
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(config.Abs(s.root, s.cfg.Paths.PublicDir), "robots.txt"))
}

func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "..") || strings.HasSuffix(r.URL.Path, ".md") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/assets/")
	if !strings.Contains(path, ".assets/") {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(path, ".drafts/") && !s.auth.Authenticated(r) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(config.Abs(s.root, s.cfg.Paths.ContentDir), filepath.FromSlash(path))
	serveStaticFile(w, r, full)
}
