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

func TestAdminAPIContentRequiresSession(t *testing.T) {
	srv := testServer(t)
	res := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/posts", `{"title":"Post"}`, "", false)
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestAdminAPIPostCreatePatchConflictAndDelete(t *testing.T) {
	srv := testServer(t)
	created := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/posts", `{
		"title":"REST Post","description":"Desc","date":"2026-08-03T10:00:00Z",
		"publish":false,"slug":"rest-post","category":"Go","tags":["api"],
		"cover_image":"/cover.png","toc":true,"template":"post","weight":2,"body":"Body"
	}`, "", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var response adminContentMutationResponse
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Revision == "" || created.Header().Get("ETag") != `"`+response.Revision+`"` {
		t.Fatalf("revision = %q, ETag = %q", response.Revision, created.Header().Get("ETag"))
	}
	if response.Title != "REST Post" || response.Type != "post" || response.AssetPrefix == "" {
		t.Fatalf("response = %+v", response)
	}

	itemPath := "/api/admin/v1/posts/" + response.ID
	patched := performAdminContentRequest(t, srv, http.MethodPatch, itemPath, `{"title":"Updated"}`, created.Header().Get("ETag"), true)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	var updated adminContentMutationResponse
	if err := json.NewDecoder(patched.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated" || updated.Body != "Body" || updated.Revision == response.Revision || updated.Path != response.Path {
		t.Fatalf("updated = %+v", updated)
	}

	conflict := performAdminContentRequest(t, srv, http.MethodPatch, itemPath, `{"body":"stale"}`, created.Header().Get("ETag"), true)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "revision_conflict") {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	deleted := performAdminContentRequest(t, srv, http.MethodDelete, itemPath, "", patched.Header().Get("ETag"), true)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
}

func TestAdminAPIContentValidationAndSlugConflict(t *testing.T) {
	srv := testServer(t)
	badDate := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/pages", `{"title":"Page","slug":"page","date":"2026-08-03"}`, "", true)
	if badDate.Code != http.StatusUnprocessableEntity || !strings.Contains(badDate.Body.String(), `"date"`) {
		t.Fatalf("bad date status = %d, body = %s", badDate.Code, badDate.Body.String())
	}

	first := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/pages", `{"title":"One","slug":"same","date":"2026-08-03T10:00:00Z"}`, "", true)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	duplicate := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/pages", `{"title":"Two","slug":"same","date":"2026-08-03T11:00:00Z"}`, "", true)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "slug_conflict") {
		t.Fatalf("duplicate status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestAdminAPINotePrivateFieldsStayOutOfFrontMatter(t *testing.T) {
	srv := testServer(t)
	invalid := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/notes", `{"title":"Note","body":"Secret","date":"2026-08-03T10:00:00Z","publish":true}`, "", true)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), `"publish"`) {
		t.Fatalf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}

	created := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/notes", `{"title":"Note","body":"Secret","date":"2026-08-03T10:00:00Z"}`, "", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var response adminContentMutationResponse
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	item, err := srv.repo.GetByID(response.ID, "note")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := contentRevision(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	patched := performAdminContentRequest(t, srv, http.MethodPatch, "/api/admin/v1/notes/"+response.ID, `{"body":"Changed"}`, `"`+revision+`"`, true)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	saved, err := srv.repo.GetByID(response.ID, "note")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Publish != true {
		t.Fatalf("saved note = %+v", saved)
	}
	data, err := os.ReadFile(saved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "publish:") || strings.Contains(string(data), "wiki_status:") || strings.Contains(string(data), "wiki_hash:") {
		t.Fatalf("note exposed private/admin fields:\n%s", data)
	}
}

func TestAdminAPINoteTitleRenameSyncsWikiPath(t *testing.T) {
	srv := testServer(t)
	created := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/notes", `{"title":"旧标题","body":"Secret","date":"2026-08-03T10:00:00Z"}`, "", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var response adminContentMutationResponse
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	oldPath := response.Path
	if oldPath != "content/notes/2026/2026-08-03-1-旧标题.md" {
		t.Fatalf("created note path = %q", oldPath)
	}
	wikiPath := filepath.Join(srv.root, "wiki/sources/example.md")
	writeFile(t, wikiPath, "# Example\n\n- "+oldPath+"\n")

	item, err := srv.repo.GetByID(response.ID, content.TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := content.Revision(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	patched := performAdminContentRequest(t, srv, http.MethodPatch, "/api/admin/v1/notes/"+response.ID, `{"title":"123啊啊334"}`, `"`+revision+`"`, true)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	var updated adminContentMutationResponse
	if err := json.NewDecoder(patched.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	wantPath := "content/notes/2026/2026-08-03-1-123啊啊334.md"
	if updated.Path != wantPath || updated.ID != response.ID {
		t.Fatalf("updated note = path %q, id %q; want path %q and stable id %q", updated.Path, updated.ID, wantPath, response.ID)
	}
	if _, err := os.Stat(filepath.Join(srv.root, filepath.FromSlash(oldPath))); !os.IsNotExist(err) {
		t.Fatalf("old note path still exists: %v", err)
	}
	data, err := os.ReadFile(wikiPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, oldPath) || !strings.Contains(body, wantPath) {
		t.Fatalf("wiki path references not updated: %s", body)
	}
}

func TestAdminAPICreateCommitsDraftToken(t *testing.T) {
	srv := testServer(t)
	token := strings.Repeat("a", 32)
	draftDir := filepath.Join(srv.root, "content/.drafts", token+".assets")
	writeFile(t, filepath.Join(draftDir, "image.png"), "png")
	oldPath := "/assets/.drafts/" + token + ".assets/image.png"
	created := performAdminContentRequest(t, srv, http.MethodPost, "/api/admin/v1/posts", `{
		"title":"Assets","slug":"assets","date":"2026-08-03T10:00:00Z",
		"body":"![x](`+oldPath+`)","draft_token":"`+token+`"
	}`, "", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", created.Code, created.Body.String())
	}
	var response adminContentMutationResponse
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body, oldPath) || !strings.Contains(response.Body, response.AssetPrefix+"/image.png") {
		t.Fatalf("body = %q, asset_prefix = %q", response.Body, response.AssetPrefix)
	}
	if _, err := os.Stat(filepath.Join(srv.root, "content/posts", filepath.FromSlash(response.ID)+".assets/image.png")); err != nil {
		t.Fatal(err)
	}
}

func performAdminContentRequest(t *testing.T, srv *Server, method, target, body, ifMatch string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	if authenticated {
		loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/session", strings.NewReader(`{"password":"secret"}`))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRes := httptest.NewRecorder()
		srv.Handler().ServeHTTP(loginRes, loginReq)
		var session adminSessionResponse
		if err := json.NewDecoder(loginRes.Body).Decode(&session); err != nil {
			t.Fatal(err)
		}
		req.AddCookie(loginRes.Result().Cookies()[0])
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
	}
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}

func contentRevision(path string) (string, error) {
	return content.Revision(path)
}

func saveInputFromAPIItem(item *content.Item) content.SaveInput {
	return content.SaveInput{
		ID: item.ID, Type: item.Type, Title: item.Title, Description: item.Description, Date: item.Date,
		Publish: item.Publish, Slug: item.Slug, Category: item.Category, Tags: item.Tags,
		CoverImage: item.CoverImage, TOC: item.TOC, Template: item.Template, SourceURL: item.SourceURL,
		Weight: item.Weight, Body: item.Body,
	}
}
