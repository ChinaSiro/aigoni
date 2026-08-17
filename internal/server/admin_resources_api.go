package server

import (
	"errors"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"aigoni/internal/content"
)

// registerAdminAssetsAPIRoutes adds the resource REST contract. The caller owns
// the shared Admin API middleware and calls this while building its mux.
func (s *Server) registerAdminAssetsAPIRoutes(mux *http.ServeMux) {
	mux.Handle("POST "+adminAPIBasePath+"/drafts", s.requireAdminAPIAuth(http.HandlerFunc(s.adminResourcesAPI)))
	mux.Handle("GET "+adminAPIBasePath+"/drafts/", s.requireAdminAPIAuth(http.HandlerFunc(s.adminResourcesAPI)))
	mux.Handle("POST "+adminAPIBasePath+"/drafts/", s.requireAdminAPIAuth(http.HandlerFunc(s.adminResourcesAPI)))
	mux.Handle("PUT "+adminAPIBasePath+"/drafts/", s.requireAdminAPIAuth(http.HandlerFunc(s.adminResourcesAPI)))
	mux.Handle("DELETE "+adminAPIBasePath+"/drafts/", s.requireAdminAPIAuth(http.HandlerFunc(s.adminResourcesAPI)))
}

// adminResourcesAPI serves draft and content resource REST endpoints after the
// shared Admin API middleware authenticates the request and validates CSRF.
func (s *Server) adminResourcesAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, adminAPIBasePath+"/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "drafts" {
		if r.Method != http.MethodPost {
			writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		token, err := s.resourceService().CreateDraft()
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "failed to create draft")
			return
		}
		writeAdminJSONStatus(w, http.StatusCreated, map[string]string{
			"token":        token,
			"asset_prefix": draftAssetPrefix(token),
			"cleanup":      "removed when the draft is committed; abandoned drafts older than 7 days are cleaned automatically",
		})
		return
	}
	if len(parts) >= 2 && parts[0] == "drafts" {
		s.adminDraftResourcesAPI(w, r, parts)
		return
	}
	if len(parts) >= 3 {
		typ, ok := adminResourceType(parts[0])
		if !ok {
			writeAdminError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		s.adminContentResourcesAPI(w, r, typ, parts)
		return
	}
	writeAdminError(w, http.StatusNotFound, "not_found", "resource not found")
}

func (s *Server) adminDraftResourcesAPI(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 3 || !content.ValidDraftToken(parts[1]) || parts[2] != "assets" {
		writeAdminError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	token := parts[1]
	svc := s.resourceService()
	switch {
	case len(parts) == 3 && r.Method == http.MethodGet:
		assets, cover, err := svc.ListDraft(token)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "failed to list draft assets")
			return
		}
		writeAdminJSON(w, http.StatusOK, adminAssetList{Items: adminAssetFiles(assets), Cover: adminAssetFile(cover)})
	case len(parts) == 3 && r.Method == http.MethodPost:
		asset, err := uploadAdminAsset(w, r, svc.UploadDraft, token, "asset")
		if err != nil {
			adminResourceUploadError(w, err)
			return
		}
		writeAdminJSONStatus(w, http.StatusCreated, adminAssetFile(&asset))
	case len(parts) == 4 && parts[3] == "cover" && r.Method == http.MethodPut:
		asset, err := uploadAdminAsset(w, r, svc.UploadDraftCover, token, "cover")
		if err != nil {
			adminResourceUploadError(w, err)
			return
		}
		writeAdminJSON(w, http.StatusOK, adminAssetFile(&asset))
	case len(parts) == 4 && parts[3] == "cover" && r.Method == http.MethodDelete:
		if err := svc.DeleteDraftCover(token); err != nil {
			adminResourceDeleteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 4 && r.Method == http.MethodDelete:
		name, err := url.PathUnescape(parts[3])
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid asset name")
			return
		}
		if err := svc.DeleteDraft(token, name); err != nil {
			adminResourceDeleteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) adminContentResourcesAPI(w http.ResponseWriter, r *http.Request, typ content.Type, parts []string) {
	// Content IDs are repository-relative paths such as 2026/2026-08-01-2.
	// The REST marker is the final "assets" segment, so do not assume the ID
	// occupies one URL segment.
	assetIndex := -1
	for i := len(parts) - 1; i >= 2; i-- {
		if parts[i] == "assets" {
			assetIndex = i
			break
		}
	}
	if assetIndex < 2 {
		writeAdminError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	id, err := url.PathUnescape(strings.Join(parts[1:assetIndex], "/"))
	if err != nil || id == "" || strings.Contains(id, "\\") {
		writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid content id")
		return
	}
	suffix := parts[assetIndex+1:]
	svc := s.resourceService()
	switch {
	case len(suffix) == 0 && r.Method == http.MethodGet:
		assets, cover, err := svc.List(id, typ)
		if err != nil {
			adminResourceLookupError(w, err)
			return
		}
		writeAdminJSON(w, http.StatusOK, adminAssetList{Items: adminAssetFiles(assets), Cover: adminAssetFile(cover)})
	case len(suffix) == 0 && r.Method == http.MethodPost:
		asset, err := uploadAdminAsset(w, r, func(_ string, h *multipart.FileHeader) (content.AssetFile, error) {
			return svc.Upload(id, typ, h)
		}, "", "asset")
		if err != nil {
			adminResourceUploadError(w, err)
			return
		}
		writeAdminJSONStatus(w, http.StatusCreated, adminAssetFile(&asset))
	case len(suffix) == 1 && suffix[0] == "download" && r.Method == http.MethodPost:
		if err := parseAssetForm(r); err != nil {
			writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		asset, err := s.downloadRemoteImage(r, id, typ, r.FormValue("url"), r.FormValue("name"))
		if err != nil {
			if errors.Is(err, errInvalidRemoteImageURL) || errors.Is(err, errRemoteImageTooLarge) || errors.Is(err, errRemoteImageNotImage) || errors.Is(err, content.ErrAssetTooLarge) {
				writeAdminError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeAdminError(w, http.StatusBadGateway, "remote_download_failed", err.Error())
			return
		}
		writeAdminJSONStatus(w, http.StatusCreated, adminAssetFile(&asset))
	case len(suffix) == 1 && suffix[0] == "cover" && r.Method == http.MethodPut:
		asset, err := uploadAdminAsset(w, r, func(_ string, h *multipart.FileHeader) (content.AssetFile, error) {
			return svc.UploadCover(id, typ, h)
		}, "", "cover")
		if err != nil {
			adminResourceUploadError(w, err)
			return
		}
		writeAdminJSON(w, http.StatusOK, adminAssetFile(&asset))
	case len(suffix) == 1 && suffix[0] == "cover" && r.Method == http.MethodDelete:
		if err := svc.DeleteCover(id, typ); err != nil {
			adminResourceLookupError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(suffix) == 1 && r.Method == http.MethodDelete:
		name, err := url.PathUnescape(suffix[0])
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "validation_failed", "invalid asset name")
			return
		}
		if err := svc.Delete(id, typ, name); err != nil {
			adminResourceDeleteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
