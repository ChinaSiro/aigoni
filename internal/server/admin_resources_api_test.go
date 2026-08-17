package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aigoni/internal/content"
)

func TestAdminResourceAPIDraftLifecycle(t *testing.T) {
	srv := testServer(t)
	mux := http.NewServeMux()
	srv.registerAdminAssetsAPIRoutes(mux)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/drafts", nil)
	addAdminAPIAuth(t, srv, req)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body=%s", res.Code, res.Body.String())
	}
	var draft struct {
		Token       string `json:"token"`
		AssetPrefix string `json:"asset_prefix"`
		Cleanup     string `json:"cleanup"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if !content.ValidDraftToken(draft.Token) || draft.AssetPrefix != draftAssetPrefix(draft.Token) || !strings.Contains(draft.Cleanup, "7 days") {
		t.Fatalf("draft = %#v", draft)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("asset", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/drafts/"+draft.Token+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	addAdminAPIAuth(t, srv, req)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/drafts/"+draft.Token+"/assets", nil)
	addAdminAPIReadAuth(t, srv, req)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"size":3`)) {
		t.Fatalf("list status=%d, body=%s", res.Code, res.Body.String())
	}
}

func TestAdminResourceAPIListsAssetsForNestedContentID(t *testing.T) {
	srv := testServer(t)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/posts/2024/2024-01-01-1/assets", nil)
	addAdminAPIReadAuth(t, srv, req)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("body = %s", res.Body.String())
	}
}

func TestAdminResourceAPIRejectsOversizedUpload(t *testing.T) {
	srv := testServer(t)
	mux := http.NewServeMux()
	srv.registerAdminAssetsAPIRoutes(mux)
	token, err := srv.resourceService().CreateDraft()
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("asset", "large.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte{'x'}, int(20<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/drafts/"+token+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	addAdminAPIAuth(t, srv, req)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", res.Code, res.Body.String())
	}
}

func addAdminAPIAuth(t *testing.T, srv *Server, req *http.Request) {
	t.Helper()
	cookie, csrf := createAdminAPISession(t, srv)
	req.AddCookie(cookie)
	req.Header.Set("X-CSRF-Token", csrf)
}

func addAdminAPIReadAuth(t *testing.T, srv *Server, req *http.Request) {
	t.Helper()
	cookie, _ := createAdminAPISession(t, srv)
	req.AddCookie(cookie)
}

func createAdminAPISession(t *testing.T, srv *Server) (*http.Cookie, string) {
	t.Helper()
	body := bytes.NewBufferString(`{"password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.adminSession(res, req)
	if res.Code != http.StatusOK && res.Code != http.StatusCreated {
		t.Fatalf("session status = %d, body=%s", res.Code, res.Body.String())
	}
	cookies := res.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session endpoint did not set a cookie")
	}
	var response struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CSRFToken == "" {
		t.Fatalf("session response has no csrf_token: %s", res.Body.String())
	}
	return cookies[0], response.CSRFToken
}
