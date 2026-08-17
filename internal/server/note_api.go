package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"aigoni/internal/content"
)

// apiBodyLimit API 单次 JSON 请求体上限（2MiB）。
const apiBodyLimit = 2 << 20

var markdownRemoteImagePattern = regexp.MustCompile(`(!\[[^\]]*\]\()(https?://[^\s)]+)([^)]*\))`)

// contentAPIRequest 统一机器写入请求：内容元信息默认从 body 的 YAML frontmatter 读取，
// JSON 层保留类型、正文、图片同步开关，以及仅对 post/page 生效的 publish 开关。
type contentAPIRequest struct {
	Type       string `json:"type"`
	Body       string `json:"body"`
	SyncImages bool   `json:"sync_images"`
	Publish    *bool  `json:"publish"`
}

type contentAPIResponse struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Title        string `json:"title"`
	Date         string `json:"date"`
	Slug         string `json:"slug,omitempty"`
	Publish      *bool  `json:"publish,omitempty"`
	TOC          *bool  `json:"toc,omitempty"`
	SyncedImages int    `json:"synced_images"`
}

// requireAPIKey 校验 .env 中 AIGONI_API_KEY：支持 X-API-Key 头或 Authorization: Bearer <key>。
// 未配置 key 时拒绝所有请求。
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := s.env.AigoniAPIKey
		if expected == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "api key not configured")
			return
		}
		provided := r.Header.Get("X-API-Key")
		if provided == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				provided = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeAPIError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiCreateContent POST /api/content：创建 note/post/page。请求接受 type、body、sync_images，
// post/page 还可传 publish；标题、slug、tags 等元信息从 body 的 YAML frontmatter 读取。
// sync_images 为 true 时同步 Markdown 网络图片，失败时删除本次新建内容。
func (s *Server) apiCreateContent(w http.ResponseWriter, r *http.Request) {
	var req contentAPIRequest
	if !decodeAPIRequest(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeAPIError(w, http.StatusBadRequest, "body is required")
		return
	}
	typ, ok := apiContentType(req.Type)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid type")
		return
	}
	body := req.Body
	if req.Publish != nil {
		if typ == content.TypeNote {
			writeAPIError(w, http.StatusBadRequest, "publish is not allowed for note")
			return
		}
		var err error
		body, err = apiBodyWithPublish(body, *req.Publish)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	item, err := content.Parse([]byte(body), typ)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	saved, err := s.repo.Save(apiSaveInputFromItem(item))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	synced, err := s.syncAPIImages(r, saved, req.SyncImages)
	if err != nil {
		s.cleanupAPIContent(saved.ID, typ)
		writeAPIError(w, remoteImageAPIStatus(err), err.Error())
		return
	}
	writeAPIContentResponse(w, saved, synced)
}

// apiContentType 把请求中的 type 枚举映射为内容类型，未知值返回 false。
func apiContentType(value string) (content.Type, bool) {
	switch value {
	case "note":
		return content.TypeNote, true
	case "post":
		return content.TypePost, true
	case "page":
		return content.TypePage, true
	default:
		return "", false
	}
}

func apiBodyWithPublish(body string, publish bool) (string, error) {
	fm, markdownBody, err := apiSplitFrontMatter(body)
	if err != nil {
		return "", err
	}
	var meta map[string]any
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return "", fmt.Errorf("front matter parse failed: %w", err)
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["publish"] = publish
	data, err := yaml.Marshal(meta)
	if err != nil {
		return "", err
	}
	return "---\n" + string(data) + "---\n\n" + markdownBody, nil
}

func apiSplitFrontMatter(data string) (string, string, error) {
	if !strings.HasPrefix(data, "---\n") && !strings.HasPrefix(data, "---\r\n") {
		return "", "", errors.New("front matter is required")
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	parts := strings.SplitN(strings.TrimPrefix(normalized, "---\n"), "\n---\n", 2)
	if len(parts) != 2 {
		return "", "", errors.New("front matter closing marker is required")
	}
	return parts[0], strings.TrimPrefix(parts[1], "\n"), nil
}

func decodeAPIRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, apiBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func (s *Server) syncAPIImages(r *http.Request, item *content.Item, enabled bool) (int, error) {
	if !enabled {
		return 0, nil
	}
	body, count, err := s.localizeMarkdownImages(r, item.ID, item.Type, item.Body)
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	input := apiSaveInputFromItem(item)
	input.Body = body
	updated, err := s.repo.Save(input)
	if err != nil {
		return 0, err
	}
	*item = *updated
	return count, nil
}

func (s *Server) localizeMarkdownImages(r *http.Request, id string, typ content.Type, body string) (string, int, error) {
	matches := markdownRemoteImagePattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return body, 0, nil
	}

	replacements := make(map[string]string)
	for _, match := range matches {
		remoteURL := match[2]
		if _, ok := replacements[remoteURL]; ok {
			continue
		}
		u, err := url.Parse(remoteURL)
		if err != nil {
			return "", 0, errInvalidRemoteImageURL
		}
		name := filepath.Base(u.Path)
		asset, err := s.downloadRemoteImage(r, id, typ, remoteURL, name)
		if err != nil {
			return "", 0, err
		}
		replacements[remoteURL] = asset.Path
	}

	localized := markdownRemoteImagePattern.ReplaceAllStringFunc(body, func(image string) string {
		parts := markdownRemoteImagePattern.FindStringSubmatch(image)
		if len(parts) != 4 {
			return image
		}
		localURL, ok := replacements[parts[2]]
		if !ok {
			return image
		}
		return parts[1] + localURL + parts[3]
	})
	return localized, len(replacements), nil
}

func apiSaveInputFromItem(item *content.Item) content.SaveInput {
	return content.SaveInput{
		ID:          item.ID,
		Type:        item.Type,
		Title:       item.Title,
		Description: item.Description,
		Date:        item.Date,
		Publish:     item.Publish,
		Slug:        item.Slug,
		Category:    item.Category,
		Tags:        item.Tags,
		CoverImage:  item.CoverImage,
		TOC:         item.TOC,
		Template:    item.Template,
		SourceURL:   item.SourceURL,
		Weight:      item.Weight,
		Body:        item.Body,
	}
}

func (s *Server) cleanupAPIContent(id string, typ content.Type) {
	_ = s.repo.Delete(id, typ)
}

func remoteImageAPIStatus(err error) int {
	if errors.Is(err, errInvalidRemoteImageURL) || errors.Is(err, errRemoteImageTooLarge) || errors.Is(err, errRemoteImageNotImage) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func writeAPIContentResponse(w http.ResponseWriter, saved *content.Item, syncedImages int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	response := contentAPIResponse{
		ID:           saved.ID,
		Path:         saved.ID + ".md",
		Title:        saved.Title,
		Date:         saved.Date.UTC().Format("2006-01-02T15:04:05Z"),
		Slug:         saved.Slug,
		SyncedImages: syncedImages,
	}
	if saved.Type != content.TypeNote {
		publish := saved.Publish
		toc := saved.TOC
		response.Publish = &publish
		response.TOC = &toc
	}
	_ = json.NewEncoder(w).Encode(response)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
