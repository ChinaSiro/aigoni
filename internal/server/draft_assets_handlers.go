package server

import (
	"net/http"

	"aigoni/internal/content"
)

func (s *Server) draftAssets(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) != 4 || parts[0] != "drafts" || parts[2] != "assets" {
		return false
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return true
	}
	token := parts[1]
	action := parts[3]
	svc := s.resourceService()

	sendAsset := func(asset content.AssetFile) {
		writeJSON(w, map[string]any{
			"name":     asset.Name,
			"path":     asset.Path,
			"markdown": asset.Markdown,
			"isImage":  asset.IsImage,
		})
	}

	switch action {
	case "upload":
		if err := r.ParseMultipartForm(20 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		file, header, err := r.FormFile("asset")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		file.Close()
		asset, err := svc.UploadDraft(token, header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		sendAsset(asset)
		return true
	case "cover-upload":
		if err := r.ParseMultipartForm(20 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		file, header, err := r.FormFile("cover")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		file.Close()
		asset, err := svc.UploadDraftCover(token, header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		writeJSON(w, map[string]string{"path": asset.Path})
		return true
	case "delete":
		if err := parseAssetDeleteForm(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		name := r.FormValue("name")
		if err := svc.DeleteDraft(token, name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		writeJSON(w, map[string]string{"name": name})
		return true
	case "cover-delete":
		if err := svc.DeleteDraftCover(token); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		writeJSON(w, map[string]bool{"ok": true})
		return true
	default:
		return false
	}
}
