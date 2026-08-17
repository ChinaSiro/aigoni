package server

import (
	"encoding/json"
	"net/http"
)

const adminAPIBasePath = "/api/admin/v1"

type apiErrorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

type apiListResponse[T any] struct {
	Items      []T `json:"items"`
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func writeAdminJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeAdminErrorFields(w, status, code, message, nil)
}

func writeAdminErrorFields(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeAdminJSON(w, status, apiErrorBody{Error: apiError{
		Code:        code,
		Message:     message,
		FieldErrors: fields,
	}})
}

func writeAdminList[T any](w http.ResponseWriter, items []T, page, perPage, total int) {
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	writeAdminJSON(w, http.StatusOK, apiListResponse[T]{
		Items:      items,
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
	})
}

func (s *Server) adminAPINotFound(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Authenticated(r) {
		writeAdminError(w, http.StatusUnauthorized, "unauthenticated", "请先登录。")
		return
	}
	writeAdminError(w, http.StatusNotFound, "not_found", "接口不存在。")
}

func (s *Server) requireAdminAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Authenticated(r) {
			writeAdminError(w, http.StatusUnauthorized, "unauthenticated", "请先登录。")
			return
		}
		if isAdminWriteMethod(r.Method) && !s.auth.ValidCSRFToken(r, r.Header.Get("X-CSRF-Token")) {
			writeAdminError(w, http.StatusForbidden, "csrf_failed", "CSRF token 无效。")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAdminWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
