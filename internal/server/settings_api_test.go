package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsAPIGetUsesWhitelist(t *testing.T) {
	srv := testServer(t)
	req := adminAPIRequest(t, srv, http.MethodGet, "/api/admin/v1/settings", nil, "")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	for _, forbidden := range []string{"theme", "themes_dir", "ADMIN_PASSWORD", "AigoniAPIKey", "WikiAPIKey"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response exposed %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"uploads"`) || !strings.Contains(body, `"site_asset_path":"/uploads/site/"`) {
		t.Fatalf("response missing upload metadata: %s", body)
	}
}

func TestSettingsAPIPatchValidatesAndReloads(t *testing.T) {
	srv := testServer(t)
	body := `{"site":{"name":"Changed","utc_offset":"+08:00"},"pagination":{"posts_per_page":20},"paths":{"uploads_dir":"var/uploads"}}`
	req := adminAPIRequest(t, srv, http.MethodPatch, "/api/admin/v1/settings", strings.NewReader(body), adminCSRFToken(t, srv))
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if srv.cfg.Site.Name != "Changed" || srv.cfg.Site.UTCOffset != "+08:00" || srv.cfg.Paths.UploadsDir != "var/uploads" {
		t.Fatalf("config was not reloaded: %#v", srv.cfg)
	}
	data, err := os.ReadFile(filepath.Join(srv.root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Changed") || !strings.Contains(string(data), "var/uploads") {
		t.Fatalf("config was not saved: %s", data)
	}
}

func TestSettingsAPIPatchRejectsTrailingJSONValue(t *testing.T) {
	srv := testServer(t)
	req := adminAPIRequest(t, srv, http.MethodPatch, "/api/admin/v1/settings", strings.NewReader(`{"site":{"name":"Changed"}} {}`), adminCSRFToken(t, srv))
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "invalid_json") {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestSettingsAPIPatchRejectsUnknownAndUnsafeValues(t *testing.T) {
	srv := testServer(t)
	for _, body := range []string{
		`{"theme":{"name":"leak"}}`,
		`{"site":{"utc_offset":"UTC"}}`,
		`{"paths":{"uploads_dir":"/tmp/uploads"}}`,
	} {
		req := adminAPIRequest(t, srv, http.MethodPatch, "/api/admin/v1/settings", strings.NewReader(body), adminCSRFToken(t, srv))
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest && res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s: status = %d: %s", body, res.Code, res.Body.String())
		}
	}
}

func TestSettingsAPIRequiresSessionAndCSRF(t *testing.T) {
	srv := testServer(t)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings", nil))
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.Body.String(), "unauthenticated") {
		t.Fatalf("unauthenticated response = %d: %s", res.Code, res.Body.String())
	}
	req := adminAPIRequest(t, srv, http.MethodPatch, "/api/admin/v1/settings", strings.NewReader(`{}`), "")
	res = httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "csrf_failed") {
		t.Fatalf("csrf response = %d: %s", res.Code, res.Body.String())
	}
}

func TestSettingsAssetAPIUploadAndDelete(t *testing.T) {
	srv := testServer(t)
	token := adminCSRFToken(t, srv)
	for _, name := range []string{"logo.jpg", "logo.png"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("kind", "logo"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("asset", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("image bytes")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := adminAPIRequest(t, srv, http.MethodPut, "/api/admin/v1/settings/assets/logo", &body, token)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusCreated || !strings.Contains(res.Body.String(), `"url":"/uploads/site/logo.`) {
			t.Fatalf("upload %s = %d: %s", name, res.Code, res.Body.String())
		}
	}
	dir := filepath.Join(srv.root, "public/uploads/site")
	if _, err := os.Stat(filepath.Join(dir, "logo.jpg")); !os.IsNotExist(err) {
		t.Fatalf("old logo remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "logo.png")); err != nil {
		t.Fatal(err)
	}

	req := adminAPIRequest(t, srv, http.MethodDelete, "/api/admin/v1/settings/assets/logo", nil, token)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"deleted":true`) || srv.cfg.Site.Logo != "" {
		t.Fatalf("delete = %d: %s", res.Code, res.Body.String())
	}
	req = adminAPIRequest(t, srv, http.MethodDelete, "/api/admin/v1/settings/assets/logo", nil, token)
	res = httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"deleted":false`) {
		t.Fatalf("idempotent delete = %d: %s", res.Code, res.Body.String())
	}
}

func adminAPIRequest(t *testing.T, srv *Server, method, path string, body io.Reader, csrf string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	login := httptest.NewRecorder()
	if !srv.auth.Login(login, "10.0.0.1:1234", "secret") {
		t.Fatal("login failed")
	}
	req.AddCookie(login.Result().Cookies()[0])
	if csrf != "" {
		session, ok := srv.auth.Session(req)
		if !ok {
			t.Fatal("session missing")
		}
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
	}
	return req
}

func adminCSRFToken(t *testing.T, srv *Server) string {
	t.Helper()
	login := httptest.NewRecorder()
	if !srv.auth.Login(login, "10.0.0.1:1234", "secret") {
		t.Fatal("login failed")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(login.Result().Cookies()[0])
	session, ok := srv.auth.Session(req)
	if !ok {
		t.Fatal("session missing")
	}
	return session.CSRFToken
}

func TestSettingsAPIResponseDecodes(t *testing.T) {
	srv := testServer(t)
	req := adminAPIRequest(t, srv, http.MethodGet, "/api/admin/v1/settings", nil, "")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	var response settingsAPIResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Site.Name != "Aigoni" {
		t.Fatalf("name = %q", response.Site.Name)
	}
}
