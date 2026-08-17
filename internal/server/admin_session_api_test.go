package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminSessionAPI(t *testing.T) {
	srv := testServer(t)
	handler := srv.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/session", nil))
	assertAdminAPIError(t, unauthorized, http.StatusUnauthorized, "unauthenticated")

	invalidLogin := httptest.NewRecorder()
	invalidLoginRequest := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"wrong"}`))
	invalidLoginRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(invalidLogin, invalidLoginRequest)
	assertAdminAPIError(t, invalidLogin, http.StatusUnauthorized, "invalid_credentials")

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"secret"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	if contentType := login.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	var loggedIn adminSessionResponse
	if err := json.NewDecoder(login.Body).Decode(&loggedIn); err != nil {
		t.Fatal(err)
	}
	if !loggedIn.Authenticated || loggedIn.CSRFToken == "" || loggedIn.ExpiresAt == "" {
		t.Fatalf("response = %#v", loggedIn)
	}
	cookie := login.Result().Cookies()[0]

	get := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/session", nil)
	getRequest.AddCookie(cookie)
	handler.ServeHTTP(get, getRequest)
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
	}

	missingCSRF := httptest.NewRecorder()
	missingCSRFRequest := httptest.NewRequest(http.MethodDelete, adminAPIBasePath+"/session", nil)
	missingCSRFRequest.AddCookie(cookie)
	handler.ServeHTTP(missingCSRF, missingCSRFRequest)
	assertAdminAPIError(t, missingCSRF, http.StatusForbidden, "csrf_failed")

	logout := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodDelete, adminAPIBasePath+"/session", nil)
	logoutRequest.Header.Set("X-CSRF-Token", loggedIn.CSRFToken)
	logoutRequest.AddCookie(cookie)
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	if logout.Body.Len() != 0 {
		t.Fatalf("logout body = %q, want empty", logout.Body.String())
	}
}

func TestAdminSessionRefreshesCSRFTokenWhenLoginRotatesSession(t *testing.T) {
	srv := testServer(t)
	handler := srv.Handler()

	firstLogin := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"secret"}`))
	firstRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(firstLogin, firstRequest)
	if firstLogin.Code != http.StatusOK {
		t.Fatalf("first login status = %d, body = %s", firstLogin.Code, firstLogin.Body.String())
	}
	var firstSession adminSessionResponse
	if err := json.NewDecoder(firstLogin.Body).Decode(&firstSession); err != nil {
		t.Fatal(err)
	}
	firstCookie := firstLogin.Result().Cookies()[0]

	secondLogin := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"secret"}`))
	secondRequest.Header.Set("Content-Type", "application/json")
	secondRequest.AddCookie(firstCookie)
	handler.ServeHTTP(secondLogin, secondRequest)
	if secondLogin.Code != http.StatusOK {
		t.Fatalf("second login status = %d, body = %s", secondLogin.Code, secondLogin.Body.String())
	}
	var secondSession adminSessionResponse
	if err := json.NewDecoder(secondLogin.Body).Decode(&secondSession); err != nil {
		t.Fatal(err)
	}
	secondCookie := secondLogin.Result().Cookies()[0]
	if secondCookie.Value == firstCookie.Value {
		t.Fatal("second login reused the first session cookie")
	}

	current := httptest.NewRecorder()
	currentRequest := httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/session", nil)
	currentRequest.AddCookie(secondCookie)
	handler.ServeHTTP(current, currentRequest)
	if current.Code != http.StatusOK {
		t.Fatalf("current session status = %d, body = %s", current.Code, current.Body.String())
	}
	var currentSession adminSessionResponse
	if err := json.NewDecoder(current.Body).Decode(&currentSession); err != nil {
		t.Fatal(err)
	}
	if secondSession.CSRFToken != currentSession.CSRFToken {
		t.Fatalf("second login csrf token = %q, current session csrf token = %q", secondSession.CSRFToken, currentSession.CSRFToken)
	}
}

func TestAdminSessionLoginRateLimit(t *testing.T) {
	srv := testServer(t)
	handler := srv.Handler()
	const remote = "10.1.2.3:5678"

	// 连续 5 次错误密码后进入锁定。
	for range 5 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remote
		handler.ServeHTTP(rec, req)
		assertAdminAPIError(t, rec, http.StatusUnauthorized, "invalid_credentials")
	}

	// 锁定期间即使密码正确也返回 429。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remote
	handler.ServeHTTP(rec, req)
	assertAdminAPIError(t, rec, http.StatusTooManyRequests, "rate_limited")
}

func TestAdminSessionRejectsTrailingJSONValue(t *testing.T) {
	srv := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, adminAPIBasePath+"/session", strings.NewReader(`{"password":"secret"} {}`))
	request.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(response, request)

	assertAdminAPIError(t, response, http.StatusBadRequest, "invalid_json")
}

func TestAdminAPIUnknownRouteReturnsJSONUnauthorized(t *testing.T) {
	srv := testServer(t)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, adminAPIBasePath+"/posts", nil))
	assertAdminAPIError(t, response, http.StatusUnauthorized, "unauthenticated")
}

func assertAdminAPIError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, wantStatus, response.Body.String())
	}
	var body apiErrorBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", body.Error.Code, wantCode)
	}
}
