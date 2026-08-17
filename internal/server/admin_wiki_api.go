package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"aigoni/internal/content"
)

const adminWikiAPIBasePath = adminAPIBasePath + "/wiki"

type adminWikiRenderRequest struct {
	Markdown string `json:"markdown"`
}

type adminWikiDocument struct {
	Path string `json:"path"`
}

func (s *Server) registerAdminWikiAPIRoutes(mux *http.ServeMux) {
	handle := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, s.requireAdminAPIAuth(handler))
	}
	handle("GET "+adminWikiAPIBasePath+"/status", s.adminWikiAPIStatus)
	handle("POST "+adminWikiAPIBasePath+"/chat", s.adminWikiAPIChat)
	handle("POST "+adminWikiAPIBasePath+"/chat:stream", s.adminWikiAPIChatStream)
	handle("GET "+adminWikiAPIBasePath+"/runs/{id}", s.adminWikiAPIRun)
	handle("POST "+adminWikiAPIBasePath+"/runs/{id}/cancel", s.adminWikiAPIRunCancel)
	handle("GET "+adminWikiAPIBasePath+"/runs/{id}/events", s.adminWikiAPIRunEvents)
	handle("GET "+adminWikiAPIBasePath+"/documents", s.adminWikiAPIDocuments)
	handle("GET "+adminWikiAPIBasePath+"/documents/content", s.adminWikiAPIDocumentContent)
	handle("POST "+adminWikiAPIBasePath+"/render", s.adminWikiAPIRender)
	handle("DELETE "+adminWikiAPIBasePath+"/backups", s.adminWikiAPIBackupsDelete)
}

func (s *Server) adminWikiAPIStatus(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, map[string]any{"ready": s.wikiReady()})
}

func (s *Server) adminWikiAPIDocuments(w http.ResponseWriter, _ *http.Request) {
	docs, err := s.readWikiDocs()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "documents_unavailable", err.Error())
		return
	}
	items := make([]adminWikiDocument, 0, len(docs))
	for _, doc := range docs {
		items = append(items, adminWikiDocument{Path: doc.Path})
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminWikiAPIDocumentContent(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeAdminErrorFields(w, http.StatusBadRequest, "validation_failed", "path 不能为空。", map[string]string{"path": "path 不能为空。"})
		return
	}
	data, err := s.readPreviewPath(path)
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "document_not_found", "Wiki 文档不存在或路径不允许。")
		return
	}
	body, meta := splitWikiPreviewFrontMatter(string(data))
	html, err := content.RenderMarkdown(body)
	if err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, "render_failed", err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"path": path, "content": body, "html": html, "meta": meta})
}

func (s *Server) adminWikiAPIRender(w http.ResponseWriter, r *http.Request) {
	var request adminWikiRenderRequest
	if !decodeAdminWikiJSON(w, r, &request, false) {
		return
	}
	html, err := content.RenderMarkdown(request.Markdown)
	if err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, "render_failed", err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]string{"html": html})
}

func (s *Server) adminWikiAPIBackupsDelete(w http.ResponseWriter, _ *http.Request) {
	removed, err := s.clearWikiBackups()
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "backup_delete_failed", err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"removed": removed, "message": fmt.Sprintf("已清理 %d 个 Wiki 备份目录。", removed)})
}

func decodeAdminWikiJSON(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) bool {
	body := http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return true
		}
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体必须是合法 JSON。")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体只能包含一个 JSON 对象。")
		return false
	}
	return true
}
