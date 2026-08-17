package server

import (
	"errors"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"aigoni/internal/content"
)

type adminAsset struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
	IsImage  bool   `json:"is_image"`
	IsCover  bool   `json:"is_cover"`
	Size     int64  `json:"size"`
}

type adminAssetList struct {
	Items []adminAsset `json:"items"`
	Cover *adminAsset  `json:"cover"`
}

type adminUploadFunc func(string, *multipart.FileHeader) (content.AssetFile, error)

func adminResourceType(raw string) (content.Type, bool) {
	switch raw {
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

func draftAssetPrefix(token string) string {
	return "/assets/.drafts/" + token + ".assets"
}

func adminAssetFile(asset *content.AssetFile) *adminAsset {
	if asset == nil {
		return nil
	}
	return &adminAsset{
		Name:     asset.Name,
		Path:     asset.Path,
		Markdown: asset.Markdown,
		IsImage:  asset.IsImage,
		IsCover:  asset.IsCover,
		Size:     asset.Size,
	}
}

func adminAssetFiles(assets []content.AssetFile) []adminAsset {
	items := make([]adminAsset, 0, len(assets))
	for i := range assets {
		items = append(items, *adminAssetFile(&assets[i]))
	}
	return items
}

func uploadAdminAsset(w http.ResponseWriter, r *http.Request, upload adminUploadFunc, token, field string) (content.AssetFile, error) {
	r.Body = http.MaxBytesReader(w, r.Body, content.MaxAssetSize+(1<<20))
	if err := r.ParseMultipartForm(content.MaxAssetSize + (1 << 20)); err != nil {
		return content.AssetFile{}, err
	}
	file, header, err := r.FormFile(field)
	if err != nil {
		return content.AssetFile{}, err
	}
	if err := file.Close(); err != nil {
		return content.AssetFile{}, err
	}
	return upload(token, header)
}

func adminResourceUploadError(w http.ResponseWriter, err error) {
	if errors.Is(err, content.ErrAssetTooLarge) || strings.Contains(err.Error(), "request body too large") {
		writeAdminError(w, http.StatusRequestEntityTooLarge, "file_too_large", "asset exceeds the 20 MiB limit")
		return
	}
	writeAdminError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
}

func adminResourceDeleteError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
		writeAdminError(w, http.StatusNotFound, "not_found", "asset not found")
		return
	}
	writeAdminError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
}

func adminResourceLookupError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
		writeAdminError(w, http.StatusNotFound, "not_found", "content not found")
		return
	}
	writeAdminError(w, http.StatusInternalServerError, "internal_error", "resource operation failed")
}

func writeAdminJSONStatus(w http.ResponseWriter, status int, value any) {
	writeAdminJSON(w, status, value)
}
