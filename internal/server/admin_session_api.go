package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type adminSessionRequest struct {
	Password string `json:"password"`
}

type adminSessionResponse struct {
	Authenticated bool     `json:"authenticated"`
	ExpiresAt     string   `json:"expires_at"`
	Capabilities  []string `json:"capabilities"`
	CSRFToken     string   `json:"csrf_token"`
}

func (s *Server) adminSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.adminSessionCreate(w, r)
	case http.MethodGet:
		s.adminSessionGet(w, r)
	case http.MethodDelete:
		s.adminSessionDelete(w, r)
	default:
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法。")
	}
}

func (s *Server) adminSessionCreate(w http.ResponseWriter, r *http.Request) {
	var input adminSessionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体必须是有效 JSON。")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体只能包含一个 JSON 对象。")
		return
	}
	if strings.TrimSpace(input.Password) == "" {
		writeAdminErrorFields(w, http.StatusUnprocessableEntity, "validation_failed", "请求字段不合法。", map[string]string{"password": "密码不能为空。"})
		return
	}
	if blocked, _ := s.auth.LoginBlocked(r.RemoteAddr); blocked {
		writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "尝试次数过多，请稍后再试。")
		return
	}
	if !s.auth.Login(w, r.RemoteAddr, input.Password) {
		writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "密码不正确。")
		return
	}

	// Login stores the session before sending its cookie, so the current request can read it.
	// Replace any old cookie on the clone so the response token comes from the new session.
	cookie := loginCookie(w)
	if cookie == nil {
		writeAdminError(w, http.StatusInternalServerError, "session_create_failed", "无法创建会话。")
		return
	}
	r = r.Clone(r.Context())
	r.Header.Set("Cookie", cookie.String())
	session, ok := s.auth.Session(r)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "session_create_failed", "无法创建会话。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminSessionResponse{
		Authenticated: true,
		ExpiresAt:     session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		Capabilities:  []string{"admin"},
		CSRFToken:     session.CSRFToken,
	})
}

func (s *Server) adminSessionGet(w http.ResponseWriter, r *http.Request) {
	session, ok := s.auth.Session(r)
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "unauthenticated", "请先登录。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminSessionResponse{
		Authenticated: true,
		ExpiresAt:     session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		Capabilities:  []string{"admin"},
		CSRFToken:     session.CSRFToken,
	})
}

func (s *Server) adminSessionDelete(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func loginCookie(w http.ResponseWriter) *http.Cookie {
	for _, value := range w.Header().Values("Set-Cookie") {
		parts := strings.SplitN(value, ";", 2)
		name, token, ok := strings.Cut(parts[0], "=")
		if ok && strings.TrimSpace(name) == "aigoni_session" {
			return &http.Cookie{Name: "aigoni_session", Value: strings.TrimSpace(token)}
		}
	}
	return nil
}
