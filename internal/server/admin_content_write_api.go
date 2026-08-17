package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"aigoni/internal/content"
)

const adminAPIBase = adminAPIBasePath

type adminContentPayload struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Date        *string   `json:"date"`
	Publish     *bool     `json:"publish"`
	Slug        *string   `json:"slug"`
	Category    *string   `json:"category"`
	Tags        *[]string `json:"tags"`
	CoverImage  *string   `json:"cover_image"`
	TOC         *bool     `json:"toc"`
	Template    *string   `json:"template"`
	SourceURL   *string   `json:"source_url"`
	Weight      *int      `json:"weight"`
	Body        *string   `json:"body"`
	Revision    string    `json:"revision"`
	DraftToken  string    `json:"draft_token"`
}

type adminContentMutationResponse struct {
	ID          string       `json:"id"`
	Path        string       `json:"path"`
	Type        content.Type `json:"type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Date        time.Time    `json:"date"`
	Lastmod     time.Time    `json:"lastmod"`
	Publish     bool         `json:"publish,omitempty"`
	Slug        string       `json:"slug,omitempty"`
	Category    string       `json:"category,omitempty"`
	Tags        []string     `json:"tags"`
	CoverImage  string       `json:"cover_image,omitempty"`
	TOC         bool         `json:"toc,omitempty"`
	Template    string       `json:"template,omitempty"`
	SourceURL   string       `json:"source_url,omitempty"`
	Weight      int          `json:"weight,omitempty"`
	Body        string       `json:"body"`
	HTML        string       `json:"html"`
	Revision    string       `json:"revision"`
	SavedAt     time.Time    `json:"saved_at"`
	EditURL     string       `json:"edit_url"`
	AssetPrefix string       `json:"asset_prefix"`
}

type adminAPIErrorEnvelope struct {
	Error adminAPIError `json:"error"`
}

type adminAPIError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

func (s *Server) requireAdminContentAPI(next http.Handler) http.Handler {
	return s.requireAdminAPIAuth(next)
}

func (s *Server) adminAPIContent(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, adminAPIBase), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		adminContentWriteNotFound(w)
		return
	}
	typ, ok := adminAPIContentType(parts[0])
	if !ok {
		adminContentWriteNotFound(w)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPost {
		s.adminAPICreateContent(w, r, typ)
		return
	}
	if len(parts) >= 2 && r.Method == http.MethodPatch {
		s.adminAPIUpdateContent(w, r, typ, strings.Join(parts[1:], "/"))
		return
	}
	if len(parts) >= 2 && r.Method == http.MethodDelete {
		s.adminAPIDeleteContent(w, r, typ, strings.Join(parts[1:], "/"))
		return
	}
	adminContentWriteNotFound(w)
}

func (s *Server) adminAPICreateContent(w http.ResponseWriter, r *http.Request, typ content.Type) {
	var payload adminContentPayload
	if !decodeAdminJSON(w, r, &payload) {
		return
	}
	input, fieldErrors := createInput(payload, typ)
	if len(fieldErrors) != 0 {
		writeAdminAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "请求字段不合法", fieldErrors)
		return
	}

	s.contentWriteMu.Lock()
	defer s.contentWriteMu.Unlock()
	service := content.NewService(s.repo)
	item, revision, err := service.Create(input)
	if err != nil {
		s.writeAdminContentError(w, err)
		return
	}
	if payload.DraftToken != "" {
		body, cover, err := s.resourceService().CommitDraft(payload.DraftToken, item, item.Body, item.CoverImage)
		if err != nil {
			_ = s.repo.Delete(item.ID, typ)
			writeAdminAPIError(w, http.StatusUnprocessableEntity, "draft_commit_failed", err.Error(), map[string]string{"draft_token": err.Error()})
			return
		}
		if body != item.Body || cover != item.CoverImage {
			input.ID = item.ID
			input.Body = body
			input.CoverImage = cover
			item, revision, err = service.Update(input, revision)
			if err != nil {
				s.writeAdminContentError(w, err)
				return
			}
		}
	}
	w.Header().Set("Location", adminAPIBase+"/"+partsForType(typ)+"/"+url.PathEscape(content.StableID(item.ID, item.Type)))
	writeAdminContentMutationResponse(w, http.StatusCreated, s.adminContentResult(item, revision))
}

func (s *Server) adminAPIUpdateContent(w http.ResponseWriter, r *http.Request, typ content.Type, encodedID string) {
	id, err := decodeAdminContentID(encodedID)
	if err != nil {
		adminContentWriteNotFound(w)
		return
	}
	var payload adminContentPayload
	if !decodeAdminJSON(w, r, &payload) {
		return
	}

	s.contentWriteMu.Lock()
	defer s.contentWriteMu.Unlock()
	current, err := s.repo.GetByID(id, typ)
	if err != nil {
		s.writeAdminContentError(w, err)
		return
	}
	oldNotePath := ""
	if typ == content.TypeNote {
		oldNotePath = noteSourcePath(s.root, current)
	}
	input, fieldErrors := patchInput(payload, current)
	if len(fieldErrors) != 0 {
		writeAdminAPIError(w, http.StatusUnprocessableEntity, "validation_failed", "请求字段不合法", fieldErrors)
		return
	}
	expected := strings.TrimSpace(r.Header.Get("If-Match"))
	if expected == "" {
		expected = payload.Revision
	}
	item, revision, err := content.NewService(s.repo).Update(input, expected)
	if err != nil {
		s.writeAdminContentError(w, err)
		return
	}
	if typ == content.TypeNote {
		if err := s.syncWikiNoteRename(oldNotePath, noteSourcePath(s.root, item)); err != nil {
			s.writeAdminContentError(w, err)
			return
		}
	}
	writeAdminContentMutationResponse(w, http.StatusOK, s.adminContentResult(item, revision))
}

func (s *Server) adminAPIDeleteContent(w http.ResponseWriter, r *http.Request, typ content.Type, encodedID string) {
	id, err := decodeAdminContentID(encodedID)
	if err != nil {
		adminContentWriteNotFound(w)
		return
	}
	s.contentWriteMu.Lock()
	defer s.contentWriteMu.Unlock()
	if err := content.NewService(s.repo).Delete(id, typ, r.Header.Get("If-Match")); err != nil {
		s.writeAdminContentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func createInput(payload adminContentPayload, typ content.Type) (content.SaveInput, map[string]string) {
	fieldErrors := validatePayloadFields(payload, typ, true)
	input := content.SaveInput{Type: typ}
	applyPayload(&input, payload)
	if typ == content.TypeNote {
		input.Publish = false
		input.Slug = ""
		input.CoverImage = ""
		input.TOC = false
		input.Template = ""
		input.Weight = 0
	}
	return input, fieldErrors
}

func patchInput(payload adminContentPayload, current *content.Item) (content.SaveInput, map[string]string) {
	fieldErrors := validatePayloadFields(payload, current.Type, false)
	input := content.SaveInput{
		ID: current.ID, Type: current.Type, Title: current.Title, Description: current.Description,
		Date: current.Date, Publish: current.Publish, Slug: current.Slug, Category: current.Category,
		Tags: current.Tags, CoverImage: current.CoverImage, TOC: current.TOC, Template: current.Template,
		SourceURL: current.SourceURL, Weight: current.Weight, Body: current.Body,
	}
	applyPayload(&input, payload)
	if current.Type == content.TypeNote {
		input.Publish = false
		input.Slug = ""
		input.CoverImage = ""
		input.TOC = false
		input.Template = ""
		input.Weight = 0
	}
	if current.Type != content.TypeNote && strings.TrimSpace(input.Title) == "" {
		fieldErrors["title"] = "标题不能为空"
	}
	if current.Type != content.TypeNote && strings.TrimSpace(input.Slug) == "" {
		fieldErrors["slug"] = "路径标识不能为空"
	}
	if current.Type == content.TypeNote && strings.TrimSpace(input.Title) == "" && strings.TrimSpace(input.Body) == "" {
		fieldErrors["body"] = "笔记标题和正文不能同时为空"
	}
	return input, fieldErrors
}

func validatePayloadFields(payload adminContentPayload, typ content.Type, create bool) map[string]string {
	errs := map[string]string{}
	if create && payload.Date == nil {
		errs["date"] = "日期不能为空"
	}
	if payload.Date != nil {
		if _, err := time.Parse(time.RFC3339, *payload.Date); err != nil {
			errs["date"] = "日期必须是 RFC3339 格式"
		}
	}
	if typ == content.TypeNote {
		for field, supplied := range map[string]bool{
			"publish": payload.Publish != nil, "slug": payload.Slug != nil, "cover_image": payload.CoverImage != nil,
			"toc": payload.TOC != nil, "template": payload.Template != nil, "weight": payload.Weight != nil,
		} {
			if supplied {
				errs[field] = "笔记不接受该字段"
			}
		}
	} else {
		if create && (payload.Title == nil || strings.TrimSpace(*payload.Title) == "") {
			errs["title"] = "标题不能为空"
		}
		if create && (payload.Slug == nil || strings.TrimSpace(*payload.Slug) == "") {
			errs["slug"] = "路径标识不能为空"
		}
		if payload.Title != nil && strings.TrimSpace(*payload.Title) == "" {
			errs["title"] = "标题不能为空"
		}
		if payload.Slug != nil && strings.TrimSpace(*payload.Slug) == "" {
			errs["slug"] = "路径标识不能为空"
		}
	}
	if create && typ == content.TypeNote {
		title, body := "", ""
		if payload.Title != nil {
			title = *payload.Title
		}
		if payload.Body != nil {
			body = *payload.Body
		}
		if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
			errs["body"] = "笔记标题和正文不能同时为空"
		}
	}
	if payload.DraftToken != "" && !content.ValidDraftToken(payload.DraftToken) {
		errs["draft_token"] = "draft_token 格式不正确"
	}
	return errs
}

func applyPayload(input *content.SaveInput, payload adminContentPayload) {
	if payload.Title != nil {
		input.Title = strings.TrimSpace(*payload.Title)
	}
	if payload.Description != nil {
		input.Description = *payload.Description
	}
	if payload.Date != nil {
		input.Date, _ = time.Parse(time.RFC3339, *payload.Date)
	}
	if payload.Publish != nil {
		input.Publish = *payload.Publish
	}
	if payload.Slug != nil {
		input.Slug = strings.TrimSpace(*payload.Slug)
	}
	if payload.Category != nil {
		input.Category = strings.TrimSpace(*payload.Category)
	}
	if payload.Tags != nil {
		input.Tags = append([]string(nil), (*payload.Tags)...)
	}
	if payload.CoverImage != nil {
		input.CoverImage = *payload.CoverImage
	}
	if payload.TOC != nil {
		input.TOC = *payload.TOC
	}
	if payload.Template != nil {
		input.Template = *payload.Template
	}
	if payload.SourceURL != nil {
		input.SourceURL = *payload.SourceURL
	}
	if payload.Weight != nil {
		input.Weight = *payload.Weight
	}
	if payload.Body != nil {
		input.Body = *payload.Body
	}
}

func (s *Server) adminContentResult(item *content.Item, revision string) adminContentMutationResponse {
	tags := item.Tags
	if tags == nil {
		tags = []string{}
	}
	return adminContentMutationResponse{
		ID: content.StableID(item.ID, item.Type), Path: filepathRelative(s.root, item.Path), Type: item.Type, Title: item.Title,
		Description: item.Description, Date: item.Date, Lastmod: item.Lastmod, Publish: item.Publish,
		Slug: item.Slug, Category: item.Category, Tags: tags, CoverImage: item.CoverImage, TOC: item.TOC,
		Template: item.Template, SourceURL: item.SourceURL, Weight: item.Weight,
		Body: item.Body, HTML: item.HTML, Revision: revision, SavedAt: time.Now().UTC(),
		EditURL:     s.adminURL(fmt.Sprintf("%ss/%s/edit", item.Type, content.StableID(item.ID, item.Type))),
		AssetPrefix: "/assets/" + string(item.Type) + "s/" + item.ID + ".assets",
	}
}

func filepathRelative(root, path string) string {
	path = strings.TrimPrefix(strings.TrimPrefix(path, root), string(os.PathSeparator))
	return strings.ReplaceAll(path, string(os.PathSeparator), "/")
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效 JSON", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid_json", "请求体只能包含一个 JSON 对象", nil)
		return false
	}
	return true
}

func decodeAdminContentID(encoded string) (string, error) {
	id, err := url.PathUnescape(encoded)
	if err != nil || id == "" || strings.Contains(id, "..") || strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
		return "", errors.New("invalid id")
	}
	return id, nil
}

func adminAPIContentType(value string) (content.Type, bool) {
	switch value {
	case "posts":
		return content.TypePost, true
	case "pages":
		return content.TypePage, true
	case "notes":
		return content.TypeNote, true
	default:
		return "", false
	}
}

func partsForType(typ content.Type) string { return string(typ) + "s" }

func writeAdminContentMutationResponse(w http.ResponseWriter, status int, response adminContentMutationResponse) {
	w.Header().Set("ETag", `"`+response.Revision+`"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) writeAdminContentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrRevisionConflict):
		writeAdminAPIError(w, http.StatusConflict, "revision_conflict", "内容已被其他请求修改", nil)
	case errors.Is(err, content.ErrSlugConflict):
		writeAdminAPIError(w, http.StatusConflict, "slug_conflict", "路径标识已存在", map[string]string{"slug": "路径标识已存在"})
	case errors.Is(err, os.ErrNotExist):
		adminContentWriteNotFound(w)
	default:
		writeAdminAPIError(w, http.StatusInternalServerError, "internal_error", "内容写入失败", nil)
	}
}

func adminContentWriteNotFound(w http.ResponseWriter) {
	writeAdminAPIError(w, http.StatusNotFound, "not_found", "内容不存在", nil)
}

func writeAdminAPIError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(adminAPIErrorEnvelope{Error: adminAPIError{Code: code, Message: message, FieldErrors: fields}})
}
