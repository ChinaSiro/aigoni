package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aigoni/internal/content"
)

func TestAPICreateNoteWithoutImageSync(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	remoteHits := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteHits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: API 笔记\ndate: 2026-08-10\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":false}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if remoteHits != 0 {
		t.Fatalf("remote hits = %d, want 0", remoteHits)
	}
	raw := res.Body.Bytes()
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["publish"]; ok {
		t.Fatalf("note response should not contain publish: %v", fields)
	}
	if _, ok := fields["toc"]; ok {
		t.Fatalf("note response should not contain toc: %v", fields)
	}
	var response contentAPIResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item.Body, remote.URL) {
		t.Fatalf("note body unexpectedly localized: %s", item.Body)
	}
}

func TestAPICreateNoteSyncsRemoteImages(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.remoteImageValidate = false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: API 笔记\ndate: 2026-08-10 13:20\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":true}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.SyncedImages != 1 {
		t.Fatalf("synced_images = %d, want 1", response.SyncedImages)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(item.Body, remote.URL) || !strings.Contains(item.Body, "/assets/notes/") {
		t.Fatalf("note body was not localized: %s", item.Body)
	}
	assets, err := filepath.Glob(strings.TrimSuffix(item.Path, ".md") + ".assets/image-*.png")
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets = %v, err = %v", assets, err)
	}
	data, err := os.ReadFile(assets[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png-data" {
		t.Fatalf("asset data = %q", data)
	}
}

func TestAPICreatePostPersistsFrontmatterAndImages(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.remoteImageValidate = false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-data"))
	}))
	defer remote.Close()

	body := `{
		"type":"post",
		"body":"---\ntitle: API Post\ndescription: Created by API\nslug: api-post\ncategory: Tech\ntags: [Go, API]\npublish: true\ntoc: true\ndate: 2026-08-10T00:00:00Z\n---\n\n## Heading\n\n![Cover](` + remote.URL + `/cover.jpg)",
		"sync_images":true
	}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Slug != "api-post" || response.Publish == nil || !*response.Publish || response.TOC == nil || !*response.TOC || response.SyncedImages != 1 {
		t.Fatalf("response = %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePost)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "API Post" || item.Slug != "api-post" || item.Category != "Tech" || len(item.Tags) != 2 || !item.Publish || !item.TOC {
		t.Fatalf("saved item = %+v", item)
	}
	if strings.Contains(item.Body, remote.URL) || !strings.Contains(item.Body, "/assets/posts/") {
		t.Fatalf("post body was not localized: %s", item.Body)
	}
}

func TestAPICreatePageFromFrontmatter(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{
		"type":"page",
		"body":"---\ntitle: About\ndescription: About page\nslug: about-page\ncategory: Info\ntags: [about]\npublish: true\ntoc: false\ndate: 2026-08-10T00:00:00Z\n---\n\nAbout body",
		"sync_images":false
	}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Slug != "about-page" || response.Publish == nil || !*response.Publish || response.TOC == nil || *response.TOC {
		t.Fatalf("response = %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePage)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "About" || item.Slug != "about-page" || item.Category != "Info" || !item.Publish || item.TOC {
		t.Fatalf("saved page = %+v", item)
	}
	if !strings.Contains(item.Body, "About body") {
		t.Fatalf("page body missing content: %s", item.Body)
	}
}

func TestAPICreateContentPreservesFrontmatterMetadata(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"note","body":"---\ntitle: 元信息笔记\ndescription: 描述\ncategory: 生活\nsource_url: https://example.com/src\ntags: [A, B]\ndate: 2026-08-10T00:00:00Z\n---\n\n正文","sync_images":false}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "元信息笔记" || item.Description != "描述" || item.Category != "生活" || item.SourceURL != "https://example.com/src" {
		t.Fatalf("note metadata not preserved: %+v", item)
	}
	if len(item.Tags) != 2 || item.Tags[0] != "A" || item.Tags[1] != "B" {
		t.Fatalf("note tags not preserved: %+v", item.Tags)
	}
}

func TestAPICreateContentRejectsInvalidType(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"video","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\nx"}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPICreateContentRejectsMissingType(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	res := performAPIRequest(t, srv, "/api/content", `{"body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\nx"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPICreateContentRejectsMissingBody(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	res := performAPIRequest(t, srv, "/api/content", `{"type":"note"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPICreateContentRejectsOldMetadataFields(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"note","title":"API 笔记","body":"---\ntitle: API 笔记\ndate: 2026-08-10T00:00:00Z\n---\n\nx"}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPICreateContentRejectsInvalidFrontmatter(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	// 没有 frontmatter 的纯文本。
	res := performAPIRequest(t, srv, "/api/content", `{"type":"note","body":"just text"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("plain body status = %d, body = %s, want 400", res.Code, res.Body.String())
	}

	// post 缺少 slug。
	res = performAPIRequest(t, srv, "/api/content", `{"type":"post","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\npublish: true\n---\n\nx"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing slug status = %d, body = %s, want 400", res.Code, res.Body.String())
	}

	// post 缺少 publish。
	res = performAPIRequest(t, srv, "/api/content", `{"type":"post","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\nslug: x\n---\n\nx"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing publish status = %d, body = %s, want 400", res.Code, res.Body.String())
	}

	// 缺少 date。
	res = performAPIRequest(t, srv, "/api/content", `{"type":"note","body":"---\ntitle: X\n---\n\nx"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing date status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPICreatePostFrontmatterDefaultsPrivate(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"post","body":"---\ntitle: Draft\nslug: draft-post\ndate: 2026-08-10T00:00:00Z\npublish: false\n---\n\nBody"}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Publish == nil || *response.Publish {
		t.Fatalf("post should be private: %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePost)
	if err != nil {
		t.Fatal(err)
	}
	if item.Publish || item.TOC {
		t.Fatalf("post should default to private without TOC: %+v", item)
	}
}

func TestAPICreatePostJSONPublishOverridesFrontmatter(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"post","body":"---\ntitle: API Post\nslug: api-post\ndate: 2026-08-10T00:00:00Z\npublish: false\n---\n\nBody","publish":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Publish == nil || !*response.Publish {
		t.Fatalf("post should be published via JSON: %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePost)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Publish {
		t.Fatalf("saved post publish = false, want true: %+v", item)
	}
}

func TestAPICreatePostJSONPublishSetsMissingFrontmatter(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"post","body":"---\ntitle: API Post\nslug: api-post\ndate: 2026-08-10T00:00:00Z\n---\n\nBody","publish":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Publish == nil || !*response.Publish {
		t.Fatalf("post should use JSON publish: %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePost)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Publish {
		t.Fatalf("saved post publish = false, want true: %+v", item)
	}
}

func TestAPICreatePageJSONPublishFalseOverridesFrontmatter(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"page","body":"---\ntitle: About\nslug: about-page\ndate: 2026-08-10T00:00:00Z\npublish: true\n---\n\nAbout body","publish":false}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response contentAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Publish == nil || *response.Publish {
		t.Fatalf("page should be private via JSON: %+v", response)
	}
	item, err := srv.repo.GetByID(response.ID, content.TypePage)
	if err != nil {
		t.Fatal(err)
	}
	if item.Publish {
		t.Fatalf("saved page publish = true, want false: %+v", item)
	}
}

func TestAPICreateNoteRejectsJSONPublish(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	body := `{"type":"note","body":"---\ntitle: 笔记\ndate: 2026-08-10T00:00:00Z\n---\n\n正文","publish":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestAPILegacyEndpointsRemoved(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	for _, path := range []string{"/api/notes", "/api/posts"} {
		res := performAPIRequest(t, srv, path, `{"type":"note","body":"---\ntitle: x\ndate: 2026-08-10T00:00:00Z\n---\n\nx"}`)
		if res.Code == http.StatusOK || res.Code == http.StatusCreated {
			t.Fatalf("%s still registered, status = %d", path, res.Code)
		}
	}
}

func TestAPIRejectsInvalidKey(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	req := httptest.NewRequest(http.MethodPost, "/api/content", strings.NewReader(`{"type":"note","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\nx"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong-key")
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestAPIImageSyncFailureRemovesCreatedContent(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.remoteImageValidate = false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "download failed", http.StatusBadGateway)
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: Bad Image\ndate: 2026-08-10T00:00:00Z\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":true}`
	res := performAPIRequest(t, srv, "/api/content", body)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	items, err := srv.repo.List(content.TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("notes count = %d, want original fixture only", len(items))
	}
}

func TestAPIRejectsTrailingJSONValue(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"

	res := performAPIRequest(t, srv, "/api/content", `{"type":"note","body":"x"} {}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func performAPIRequest(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test-key")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}
