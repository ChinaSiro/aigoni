package server

import (
	"net/http"
	"strings"

	"aigoni/internal/config"
	"aigoni/internal/content"
)

const remoteImageMaxBytes = 20 << 20

func parseAssetForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(remoteImageMaxBytes + (1 << 20))
	}
	return r.ParseForm()
}

func parseAssetDeleteForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

func (s *Server) resourceService() *content.ResourceService {
	return content.NewResourceService(
		s.repo,
		config.Abs(s.root, s.cfg.Paths.ContentDir),
		s.env.UploadAllowedExts,
	)
}
