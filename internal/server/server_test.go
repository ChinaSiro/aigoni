package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aigoni/internal/config"
)

func TestEmbeddedFrontendServesViteBuildAndSPAFallback(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = ""
	srv.webFrontendFS = os.DirFS(filepath.Join(srv.root, "frontend/dist/web"))

	tests := []struct {
		path     string
		contains string
	}{
		{path: "/", contains: "web"},
		{path: "/articles/hello", contains: "web"},
		{path: "/_app/web/index.html", contains: "web"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			res := httptest.NewRecorder()
			srv.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), test.contains) {
				t.Fatalf("body = %q, want substring %q", res.Body.String(), test.contains)
			}
		})
	}
}

func TestFrontendDirServesViteBuildAndSPAFallback(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = "frontend/dist"
	writeFile(t, filepath.Join(srv.root, "frontend/dist/index.html"), `<!doctype html><title>Vite App</title>`)
	writeFile(t, filepath.Join(srv.root, "frontend/dist/assets/app.js"), `console.log("app")`)

	tests := []struct {
		path     string
		contains string
	}{
		{path: "/", contains: "Vite App"},
		{path: "/articles/hello", contains: "Vite App"},
		{path: "/_app/web/assets/app.js", contains: `console.log("app")`},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			res := httptest.NewRecorder()
			srv.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), test.contains) {
				t.Fatalf("body = %q, want substring %q", res.Body.String(), test.contains)
			}
		})
	}
}

func TestFrontendDirKeepsBackendRoutes(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = "frontend/dist"
	writeFile(t, filepath.Join(srv.root, "frontend/dist/index.html"), `<!doctype html><title>Vite App</title>`)

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/posts", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "Vite App") {
		t.Fatalf("REST API was handled by frontend: %s", res.Body.String())
	}
}

func TestFrontendArticleInjectsServerSideSEO(t *testing.T) {
	srv := testServer(t)
	srv.cfg.Site.BaseURL = "https://envbrowser.com"
	srv.env.WebFrontendDir = "frontend/dist"
	writeFile(t, filepath.Join(srv.root, "frontend/dist/index.html"), `<!doctype html><html><head><!-- aigoni:head --><style></style></head><body></body></html>`)

	req := httptest.NewRequest(http.MethodGet, "/post/hello", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, body)
	}
	for _, want := range []string{
		"<title>Hello｜Aigoni</title>",
		`content="Desc"`,
		`href="https://envbrowser.com/post/hello"`,
		`property="og:type" content="article"`,
		`property="article:published_time"`,
		`id="article-structured-data"`,
		`"headline":"Hello"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `content="home"`) {
		t.Fatalf("home SEO remained in article response: %s", body)
	}
}

func TestFrontendArticleReturnsNotFoundForMissingPost(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = "frontend/dist"
	writeFile(t, filepath.Join(srv.root, "frontend/dist/index.html"), `<!doctype html><title>Vite App</title>`)

	req := httptest.NewRequest(http.MethodGet, "/post/missing", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestDualSPAUsesNamespacesAndPreservesBackendRoutes(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = "web/dist"
	srv.env.AdminFrontendDir = "admin/dist"
	srv.env.AdminPath = "/control"
	writeFile(t, filepath.Join(srv.root, "web/dist/index.html"), `<!doctype html><title>Web App</title>`)
	writeFile(t, filepath.Join(srv.root, "web/dist/assets/app.js"), `web asset`)
	writeFile(t, filepath.Join(srv.root, "admin/dist/index.html"), `<!doctype html><title>Admin App</title>`)
	writeFile(t, filepath.Join(srv.root, "admin/dist/assets/app.js"), `admin asset`)

	for _, test := range []struct{ path, want string }{
		{"/", "Web App"},
		{"/some-web-route", "Web App"},
		{"/_app/web/assets/app.js", "web asset"},
		{"/login", "Admin App"},
		{"/control/users", "Admin App"},
		{"/_app/admin/assets/app.js", "admin asset"},
	} {
		t.Run(test.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			srv.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, test.path, nil))
			if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), test.want) {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
		})
	}

	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/rest/v1/posts", nil))
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "Web App") {
		t.Fatalf("backend route was handled by SPA: status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestFrontendArticleHeadFallsBackWithoutPlaceholder(t *testing.T) {
	srv := testServer(t)
	srv.env.WebFrontendDir = "web/dist"
	writeFile(t, filepath.Join(srv.root, "web/dist/index.html"), `<!doctype html><html><head><title>Home</title></head><body></body></html>`)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/post/hello", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `<title>Hello｜Aigoni</title>`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestSyncWikiNoteRenameRewritesReferences(t *testing.T) {
	srv := testServer(t)
	wikiPath := filepath.Join(srv.root, "wiki/concepts/example.md")
	logPath := filepath.Join(srv.root, "wiki/log.md")
	oldRef := "content/notes/2026/2026-06-25-5-未命名笔记-qgx2y7.md"
	newRef := "content/notes/2026/2026-06-25-5-我是huo0-qgx2y7.md"
	writeFile(t, wikiPath, "# Example\n\n- "+oldRef+"\n")
	writeFile(t, logPath, "- historical: "+oldRef+"\n")

	if err := srv.syncWikiNoteRename(oldRef, newRef); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(wikiPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, oldRef) {
		t.Fatalf("old note path still exists: %s", body)
	}
	if !strings.Contains(body, newRef) {
		t.Fatalf("new note path missing: %s", body)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), oldRef) || strings.Contains(string(logData), newRef) {
		t.Fatalf("wiki log was rewritten: %s", logData)
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config.yaml"), `site:
  name: "Aigoni"
paths:
  content_dir: "content"
  posts_dir: "content/posts"
  pages_dir: "content/pages"
  notes_dir: "content/notes"
  public_dir: "public"
  uploads_dir: "public/uploads"
`)
	writeFile(t, filepath.Join(root, "content/posts/2024/2024-01-01-1.md"), `---
title: "Hello"
description: "Desc"
date: "2024-01-01T10:00:00Z"
publish: true
slug: "hello"
tags: []
---

Body
`)
	writeFile(t, filepath.Join(root, "content/notes/2024/2024-01-01-1.md"), `---
title: "Private Note"
description: "Desc"
date: "2024-01-01T10:00:00Z"
tags: []
---

Secret
`)
	writeFile(t, filepath.Join(root, "frontend/dist/web/index.html"), `<!doctype html><html><head><!-- aigoni:head --></head><body>web</body></html>`)
	writeFile(t, filepath.Join(root, "frontend/dist/admin/index.html"), `<!doctype html><html><head></head><body>admin</body></html>`)
	cfg, err := config.Load(filepath.Join(root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return New(root, cfg, &config.Env{AdminPassword: "secret", Port: 8080, WebFrontendDir: "frontend/dist/web", AdminFrontendDir: "frontend/dist/admin", UploadAllowedExts: []string{"jpg", "jpeg", "png", "webp", "gif"}, UploadAllowedExtsLabel: "jpg, jpeg, png, webp, gif", WikiAgentConcurrency: 5})
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
