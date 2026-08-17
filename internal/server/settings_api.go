package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aigoni/internal/config"
)

const settingsAssetMaxBytes = 20 << 20

type settingsAPIResponse struct {
	Site       settingsSiteResponse       `json:"site"`
	Pagination settingsPaginationResponse `json:"pagination"`
	Paths      settingsPathsResponse      `json:"paths"`
	Uploads    settingsUploadsResponse    `json:"uploads"`
	UpdatedAt  string                     `json:"updated_at,omitempty"`
}

type settingsSiteResponse struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Author       string `json:"author"`
	BaseURL      string `json:"base_url"`
	Logo         string `json:"logo"`
	AuthorAvatar string `json:"author_avatar"`
	UTCOffset    string `json:"utc_offset"`
}

type settingsPaginationResponse struct {
	PostsPerPage   int `json:"posts_per_page"`
	HomePostsCount int `json:"home_posts_count"`
}

type settingsPathsResponse struct {
	ContentDir string `json:"content_dir"`
	PostsDir   string `json:"posts_dir"`
	PagesDir   string `json:"pages_dir"`
	NotesDir   string `json:"notes_dir"`
	PublicDir  string `json:"public_dir"`
	UploadsDir string `json:"uploads_dir"`
}

type settingsUploadsResponse struct {
	AllowedExtensions []string `json:"allowed_extensions"`
	MaxBytes          int64    `json:"max_bytes"`
	SiteAssetPath     string   `json:"site_asset_path"`
}

type settingsPatchRequest struct {
	Site       *settingsSitePatch       `json:"site"`
	Pagination *settingsPaginationPatch `json:"pagination"`
	Paths      *settingsPathsPatch      `json:"paths"`
}

type settingsSitePatch struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Author       *string `json:"author"`
	BaseURL      *string `json:"base_url"`
	Logo         *string `json:"logo"`
	AuthorAvatar *string `json:"author_avatar"`
	UTCOffset    *string `json:"utc_offset"`
}

type settingsPaginationPatch struct {
	PostsPerPage   *int `json:"posts_per_page"`
	HomePostsCount *int `json:"home_posts_count"`
}

type settingsPathsPatch struct {
	ContentDir *string `json:"content_dir"`
	PostsDir   *string `json:"posts_dir"`
	PagesDir   *string `json:"pages_dir"`
	NotesDir   *string `json:"notes_dir"`
	PublicDir  *string `json:"public_dir"`
	UploadsDir *string `json:"uploads_dir"`
}

func (s *Server) settingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeAdminJSON(w, http.StatusOK, s.settingsAPIResponse(""))
		return
	}
	if r.Method != http.MethodPatch {
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法。")
		return
	}

	var patch settingsPatchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, apiBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体必须是有效 JSON。")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "请求体只能包含一个 JSON 对象。")
		return
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	next := *s.cfg
	applySettingsPatch(&next, patch)
	if fields := validateSettingsConfig(&next, s.env.UploadAllowedExts); len(fields) != 0 {
		writeAdminErrorFields(w, http.StatusUnprocessableEntity, "validation_failed", "设置校验失败。", fields)
		return
	}
	if err := s.saveAndReloadSettings(&next); err != nil {
		writeAdminError(w, http.StatusInternalServerError, "settings_save_failed", "配置保存或重载失败。")
		return
	}
	writeAdminJSON(w, http.StatusOK, s.settingsAPIResponse(time.Now().UTC().Format(time.RFC3339)))
}

func (s *Server) settingsAssetsAPI(w http.ResponseWriter, r *http.Request) {
	kind, ok := settingsAssetKindFromPath(r.PathValue("kind"))
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "invalid_asset_kind", "资源类型必须是 logo 或 avatar。")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.settingsAssetPutAPI(w, r, kind)
	case http.MethodDelete:
		s.settingsAssetDeleteAPI(w, kind)
	default:
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法。")
	}
}

func (s *Server) settingsAssetPutAPI(w http.ResponseWriter, r *http.Request, kind string) {
	r.Body = http.MaxBytesReader(w, r.Body, settingsAssetMaxBytes+1024)
	if err := r.ParseMultipartForm(settingsAssetMaxBytes); err != nil {
		status := http.StatusBadRequest
		if errors.As(err, new(*http.MaxBytesError)) {
			status = http.StatusRequestEntityTooLarge
		}
		writeAdminError(w, status, "invalid_upload", "上传文件无效或超过大小限制。")
		return
	}
	if formKind := strings.TrimSpace(r.FormValue("kind")); formKind != "" && formKind != kind {
		writeAdminError(w, http.StatusBadRequest, "invalid_asset_kind", "资源类型与请求路径不一致。")
		return
	}
	file, header, err := r.FormFile("asset")
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "missing_asset", "必须提供 asset 文件。")
		return
	}
	defer file.Close()
	name, err := fixedSettingsAssetName(kind, header.Filename, s.env.UploadAllowedExts)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_asset", "文件类型不在允许列表中。")
		return
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if err := s.storeSettingsAsset(file, kind, name); err != nil {
		writeAdminError(w, http.StatusInternalServerError, "asset_save_failed", "资源保存失败。")
		return
	}
	path := "/uploads/" + settingsAssetSubdir + "/" + name
	writeAdminJSON(w, http.StatusCreated, map[string]string{"kind": kind, "name": name, "path": path, "url": path})
}

func (s *Server) settingsAssetDeleteAPI(w http.ResponseWriter, kind string) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	dir := filepath.Join(config.Abs(s.root, s.cfg.Paths.UploadsDir), settingsAssetSubdir)
	deleted, err := removeByStemChanged(dir, kind)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "asset_delete_failed", "资源删除失败。")
		return
	}
	next := *s.cfg
	if kind == "logo" {
		next.Site.Logo = ""
	} else {
		next.Site.AuthorAvatar = ""
	}
	if err := s.saveAndReloadSettings(&next); err != nil {
		writeAdminError(w, http.StatusInternalServerError, "settings_save_failed", "配置保存或重载失败。")
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"kind": kind, "deleted": deleted})
}

func (s *Server) settingsAPIResponse(updatedAt string) settingsAPIResponse {
	return settingsAPIResponse{
		Site: settingsSiteResponse{
			Name: s.cfg.Site.Name, Description: s.cfg.Site.Description, Author: s.cfg.Site.Author,
			BaseURL: s.cfg.Site.BaseURL, Logo: s.cfg.Site.Logo, AuthorAvatar: s.cfg.Site.AuthorAvatar,
			UTCOffset: s.cfg.Site.UTCOffset,
		},
		Pagination: settingsPaginationResponse{PostsPerPage: s.cfg.Pagination.PostsPerPage, HomePostsCount: s.cfg.Pagination.HomePostsCount},
		Paths: settingsPathsResponse{
			ContentDir: s.cfg.Paths.ContentDir, PostsDir: s.cfg.Paths.PostsDir, PagesDir: s.cfg.Paths.PagesDir,
			NotesDir: s.cfg.Paths.NotesDir, PublicDir: s.cfg.Paths.PublicDir, UploadsDir: s.cfg.Paths.UploadsDir,
		},
		Uploads:   settingsUploadsResponse{AllowedExtensions: slices.Clone(s.env.UploadAllowedExts), MaxBytes: settingsAssetMaxBytes, SiteAssetPath: "/uploads/site/"},
		UpdatedAt: updatedAt,
	}
}

func applySettingsPatch(cfg *config.Config, patch settingsPatchRequest) {
	if p := patch.Site; p != nil {
		if p.Name != nil {
			cfg.Site.Name = strings.TrimSpace(*p.Name)
		}
		if p.Description != nil {
			cfg.Site.Description = strings.TrimSpace(*p.Description)
		}
		if p.Author != nil {
			cfg.Site.Author = strings.TrimSpace(*p.Author)
		}
		if p.BaseURL != nil {
			cfg.Site.BaseURL = strings.TrimSpace(*p.BaseURL)
		}
		if p.Logo != nil {
			cfg.Site.Logo = strings.TrimSpace(*p.Logo)
		}
		if p.AuthorAvatar != nil {
			cfg.Site.AuthorAvatar = strings.TrimSpace(*p.AuthorAvatar)
		}
		if p.UTCOffset != nil {
			cfg.Site.UTCOffset = strings.TrimSpace(*p.UTCOffset)
		}
	}
	if p := patch.Pagination; p != nil {
		if p.PostsPerPage != nil {
			cfg.Pagination.PostsPerPage = *p.PostsPerPage
		}
		if p.HomePostsCount != nil {
			cfg.Pagination.HomePostsCount = *p.HomePostsCount
		}
	}
	if p := patch.Paths; p != nil {
		if p.ContentDir != nil {
			cfg.Paths.ContentDir = strings.TrimSpace(*p.ContentDir)
		}
		if p.PostsDir != nil {
			cfg.Paths.PostsDir = strings.TrimSpace(*p.PostsDir)
		}
		if p.PagesDir != nil {
			cfg.Paths.PagesDir = strings.TrimSpace(*p.PagesDir)
		}
		if p.NotesDir != nil {
			cfg.Paths.NotesDir = strings.TrimSpace(*p.NotesDir)
		}
		if p.PublicDir != nil {
			cfg.Paths.PublicDir = strings.TrimSpace(*p.PublicDir)
		}
		if p.UploadsDir != nil {
			cfg.Paths.UploadsDir = strings.TrimSpace(*p.UploadsDir)
		}
	}
}

func validateSettingsConfig(cfg *config.Config, allowedExts []string) map[string]string {
	fields := map[string]string{}
	if cfg.Site.Name == "" {
		fields["site.name"] = "不能为空。"
	}
	if _, err := config.ParseOffset(cfg.Site.UTCOffset); err != nil {
		fields["site.utc_offset"] = "必须是 RFC3339 offset，例如 +08:00、-05:00 或 Z。"
	}
	if cfg.Pagination.PostsPerPage < 1 {
		fields["pagination.posts_per_page"] = "必须大于或等于 1。"
	}
	if cfg.Pagination.HomePostsCount < 1 {
		fields["pagination.home_posts_count"] = "必须大于或等于 1。"
	}
	for field, value := range map[string]string{
		"paths.content_dir": cfg.Paths.ContentDir, "paths.posts_dir": cfg.Paths.PostsDir,
		"paths.pages_dir": cfg.Paths.PagesDir, "paths.notes_dir": cfg.Paths.NotesDir,
		"paths.public_dir": cfg.Paths.PublicDir, "paths.uploads_dir": cfg.Paths.UploadsDir,
	} {
		if !safeRelativePath(value) {
			fields[field] = "必须是非空的相对路径，且不能包含 ..。"
		}
	}
	if !validSettingsAssetURL(cfg.Site.Logo, "logo", allowedExts) {
		fields["site.logo"] = "必须是本站上传的 logo URL。"
	}
	if !validSettingsAssetURL(cfg.Site.AuthorAvatar, "avatar", allowedExts) {
		fields["site.author_avatar"] = "必须是本站上传的 avatar URL。"
	}
	return fields
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	return path != "." && !strings.HasPrefix(path, ".."+string(filepath.Separator)) && path != ".."
}

func validSettingsAssetURL(value, kind string, allowedExts []string) bool {
	if value == "" {
		return true
	}
	prefix := "/uploads/site/" + kind + "."
	if !strings.HasPrefix(value, prefix) || strings.Contains(value, "?") || strings.Contains(value, "#") {
		return false
	}
	return slices.Contains(allowedExts, strings.TrimPrefix(value, prefix))
}

func settingsAssetKindFromPath(kind string) (string, bool) {
	return kind, kind == "logo" || kind == "avatar"
}

func (s *Server) saveAndReloadSettings(cfg *config.Config) error {
	configPath := filepath.Join(s.root, "config.yaml")
	if err := config.WriteAtomic(configPath, cfg); err != nil {
		return err
	}
	return s.reloadConfig()
}

func (s *Server) storeSettingsAsset(src io.Reader, kind, name string) error {
	dir := filepath.Join(config.Abs(s.root, s.cfg.Paths.UploadsDir), settingsAssetSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := removeByStemChanged(dir, kind); err != nil {
		return err
	}
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func removeByStemChanged(dir, stem string) (bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, stem+".*"))
	if err != nil {
		return false, fmt.Errorf("find old assets: %w", err)
	}
	deleted := false
	for _, path := range matches {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			if err := os.Remove(path); err != nil {
				return false, err
			}
			deleted = true
		}
	}
	return deleted, nil
}
