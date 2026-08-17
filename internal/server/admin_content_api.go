package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"aigoni/internal/content"
	"aigoni/internal/search"
)

const adminAPIDefaultPerPage = 20
const adminAPIMaxPerPage = 100

type adminContent struct {
	ID          string       `json:"id"`
	Path        string       `json:"path"`
	Type        content.Type `json:"type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Date        time.Time    `json:"date"`
	Lastmod     time.Time    `json:"lastmod"`
	Publish     bool         `json:"publish"`
	Slug        string       `json:"slug"`
	Category    string       `json:"category"`
	Tags        []string     `json:"tags"`
	CoverImage  string       `json:"cover_image"`
	TOC         bool         `json:"toc"`
	Template    string       `json:"template"`
	SourceURL   string       `json:"source_url"`
	Weight      int          `json:"weight"`
	Body        string       `json:"body,omitempty"`
	HTML        string       `json:"html,omitempty"`
	Excerpt     string       `json:"excerpt,omitempty"`
	Revision    string       `json:"revision"`
	EditURL     string       `json:"edit_url,omitempty"`
}

// categoryNone 是"未分类"筛选哨兵值：category 为空表示不过滤，
// 因此用非空哨兵代表"分类为空"。
const categoryNone = "__none__"

// categoryNoneName 是分类大全里内置"未分类"项的展示名。
const categoryNoneName = "未分类"

type adminCategory struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	None  bool   `json:"none,omitempty"`
}

type adminDashboardContentCount struct {
	Total     int `json:"total"`
	Published int `json:"published"`
	Draft     int `json:"draft"`
}

type adminDashboardResponse struct {
	Posts    adminDashboardContentCount `json:"posts"`
	Pages    adminDashboardContentCount `json:"pages"`
	Notes    int                        `json:"notes"`
	Features map[string]bool            `json:"features"`
}

func (s *Server) adminAPIDashboard(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取文章。")
		return
	}
	pages, err := s.repo.List(content.TypePage)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取页面。")
		return
	}
	notes, err := s.repo.List(content.TypeNote)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取笔记。")
		return
	}

	response := adminDashboardResponse{
		Posts: adminContentCount(posts),
		Pages: adminContentCount(pages),
		Notes: len(notes),
		Features: map[string]bool{
			"posts":           true,
			"pages":           true,
			"notes":           true,
			"search":          true,
			"categories":      true,
			"note_categories": true,
		},
	}
	writeAdminJSON(w, http.StatusOK, response)
}

func adminContentCount(items []*content.Item) adminDashboardContentCount {
	count := adminDashboardContentCount{Total: len(items)}
	for _, item := range items {
		if item.Publish {
			count.Published++
		} else {
			count.Draft++
		}
	}
	return count
}

func (s *Server) adminAPIContentList(w http.ResponseWriter, r *http.Request, typ content.Type) {
	items, err := s.repo.List(typ)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取内容。")
		return
	}
	items = adminFilterContent(items, typ, r)
	page, perPage := adminAPIPagination(r)
	pageItems, _ := restPage(items, page, perPage)
	responseItems := make([]adminContent, 0, len(pageItems))
	for _, item := range pageItems {
		result, err := adminContentFromItem(item, false, false)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法计算内容版本。")
			return
		}
		responseItems = append(responseItems, result)
	}
	writeAdminList(w, responseItems, page, perPage, len(items))
}

func adminFilterContent(items []*content.Item, typ content.Type, r *http.Request) []*content.Item {
	if typ == content.TypeNote {
		category := strings.TrimSpace(r.URL.Query().Get("category"))
		tag := strings.TrimSpace(r.URL.Query().Get("tag"))
		if category == "" && tag == "" {
			return items
		}
		out := make([]*content.Item, 0, len(items))
		for _, item := range items {
			if category == categoryNone {
				if strings.TrimSpace(item.Category) != "" {
					continue
				}
			} else if category != "" && item.Category != category {
				continue
			}
			if tag != "" && !slices.Contains(item.Tags, tag) {
				continue
			}
			out = append(out, item)
		}
		return out
	}

	publish := strings.TrimSpace(r.URL.Query().Get("publish"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if publish == "" && category == "" && tag == "" {
		return items
	}
	out := make([]*content.Item, 0, len(items))
	for _, item := range items {
		if publish == "true" && !item.Publish || publish == "false" && item.Publish {
			continue
		}
		if category == categoryNone {
			if strings.TrimSpace(item.Category) != "" {
				continue
			}
		} else if category != "" && item.Category != category {
			continue
		}
		if tag != "" && !slices.Contains(item.Tags, tag) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) adminAPIContentDetail(w http.ResponseWriter, r *http.Request, typ content.Type, prefix string) {
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if id == "" || strings.Contains(id, "..") {
		writeAdminError(w, http.StatusNotFound, "not_found", "内容不存在。")
		return
	}
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		writeAdminError(w, http.StatusNotFound, "not_found", "内容不存在。")
		return
	}
	response, err := adminContentFromItem(item, true, false)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法计算内容版本。")
		return
	}
	writeAdminJSON(w, http.StatusOK, response)
}

func adminContentFromItem(item *content.Item, detail, searchResult bool) (adminContent, error) {
	revision, err := adminRevision(item.Path)
	if err != nil {
		return adminContent{}, err
	}
	result := adminContent{
		ID:          content.StableID(item.ID, item.Type),
		Path:        item.Path,
		Type:        item.Type,
		Title:       item.Title,
		Description: item.Description,
		Date:        item.Date,
		Lastmod:     item.Lastmod,
		Publish:     item.Publish,
		Slug:        item.Slug,
		Category:    item.Category,
		Tags:        item.Tags,
		CoverImage:  item.CoverImage,
		TOC:         item.TOC,
		Template:    item.Template,
		SourceURL:   item.SourceURL,
		Weight:      item.Weight,
		Revision:    revision,
		EditURL:     adminEditURL(item),
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}
	if detail {
		result.Body = item.Body
		result.HTML = item.HTML
	}
	if searchResult {
		result.Excerpt = item.Excerpt
	}
	return result, nil
}

func adminRevision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Server) adminAPICategories(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取分类。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminCategoriesFromItems(posts))
}

func (s *Server) adminAPINoteCategories(w http.ResponseWriter, _ *http.Request) {
	notes, err := s.repo.List(content.TypeNote)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取笔记分类。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminCategoriesFromItems(notes))
}

func adminCategoriesFromItems(items []*content.Item) []adminCategory {
	counts := map[string]int{}
	uncategorized := 0
	for _, item := range items {
		name := strings.TrimSpace(item.Category)
		if name == "" {
			uncategorized++
			continue
		}
		counts[name]++
	}
	categories := make([]adminCategory, 0, len(counts)+1)
	if len(items) > 0 {
		categories = append(categories, adminCategory{Name: categoryNoneName, Count: uncategorized, None: true})
	}
	for name, count := range counts {
		categories = append(categories, adminCategory{Name: name, Count: count})
	}
	if len(categories) > 1 {
		slices.SortFunc(categories[1:], func(a, b adminCategory) int { return strings.Compare(a.Name, b.Name) })
	}
	return categories
}

func (s *Server) adminAPITags(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取标签。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminTagsFromItems(posts))
}

func (s *Server) adminAPINoteTags(w http.ResponseWriter, _ *http.Request) {
	notes, err := s.repo.List(content.TypeNote)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法读取笔记标签。")
		return
	}
	writeAdminJSON(w, http.StatusOK, adminTagsFromItems(notes))
}

func adminTagsFromItems(items []*content.Item) []adminCategory {
	counts := map[string]int{}
	for _, item := range items {
		for _, tag := range item.Tags {
			name := strings.TrimSpace(tag)
			if name != "" {
				counts[name]++
			}
		}
	}
	tags := make([]adminCategory, 0, len(counts))
	for name, count := range counts {
		tags = append(tags, adminCategory{Name: name, Count: count})
	}
	slices.SortFunc(tags, func(a, b adminCategory) int { return strings.Compare(a.Name, b.Name) })
	return tags
}

func (s *Server) adminAPISearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeAdminErrorFields(w, http.StatusBadRequest, "validation_failed", "q 不能为空。", map[string]string{"q": "不能为空"})
		return
	}
	types, ok := adminSearchTypes(strings.TrimSpace(r.URL.Query().Get("type")))
	if !ok {
		writeAdminErrorFields(w, http.StatusBadRequest, "validation_failed", "type 必须是 post、page 或 note。", map[string]string{"type": "无效类型"})
		return
	}
	var items []*content.Item
	for _, typ := range types {
		part, err := s.repo.List(typ)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法搜索内容。")
			return
		}
		items = append(items, part...)
	}
	results := search.All(items, query)
	page, perPage := adminAPIPagination(r)
	pageItems, _ := restPage(results, page, perPage)
	responseItems := make([]adminContent, 0, len(pageItems))
	for _, item := range pageItems {
		result, err := adminContentFromItem(item, false, true)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal_error", "无法计算内容版本。")
			return
		}
		responseItems = append(responseItems, result)
	}
	writeAdminList(w, responseItems, page, perPage, len(results))
}

func adminSearchTypes(raw string) ([]content.Type, bool) {
	if raw == "" {
		return []content.Type{content.TypePost, content.TypePage, content.TypeNote}, true
	}
	switch content.Type(raw) {
	case content.TypePost, content.TypePage, content.TypeNote:
		return []content.Type{content.Type(raw)}, true
	default:
		return nil, false
	}
}

func adminEditURL(item *content.Item) string {
	return adminAPIBasePath + "/" + string(item.Type) + "s/" + content.StableID(item.ID, item.Type)
}

func adminAPIPagination(r *http.Request) (int, int) {
	page := restPositiveInt(r.URL.Query().Get("page"), 1)
	perPage := restPositiveInt(r.URL.Query().Get("per_page"), adminAPIDefaultPerPage)
	return page, min(perPage, adminAPIMaxPerPage)
}
