package server

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"aigoni/internal/content"
	"aigoni/internal/search"
)

const restDefaultPerPage = 10
const restMaxPerPage = 100

type restContent struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Date        time.Time     `json:"date"`
	Lastmod     time.Time     `json:"lastmod"`
	Slug        string        `json:"slug"`
	Category    string        `json:"category,omitempty"`
	Tags        []string      `json:"tags"`
	CoverImage  string        `json:"cover_image,omitempty"`
	TOC         bool          `json:"toc,omitempty"`
	Body        string        `json:"body,omitempty"`
	HTML        string        `json:"html,omitempty"`
	Excerpt     string        `json:"excerpt,omitempty"`
	URL         string        `json:"url"`
	Canonical   string        `json:"canonical,omitempty"`
	Previous    *restPostLink `json:"previous,omitempty"`
	Next        *restPostLink `json:"next,omitempty"`
}

type restPostLink struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
	URL   string `json:"url"`
}

type restCategory struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	URL   string `json:"url"`
	None  bool   `json:"none,omitempty"`
}

type restArchive struct {
	Year  string `json:"year"`
	Count int    `json:"count"`
	URL   string `json:"url"`
}

type restListResponse struct {
	Items      []restContent `json:"items"`
	Page       int           `json:"page"`
	PerPage    int           `json:"per_page"`
	Total      int           `json:"total"`
	TotalPages int           `json:"total_pages"`
}

type restCategoryListResponse struct {
	Items []restCategory `json:"items"`
	Total int            `json:"total"`
}

type restArchiveListResponse struct {
	Items []restArchive `json:"items"`
	Total int           `json:"total"`
}

type restSiteResponse struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Author         string          `json:"author"`
	BaseURL        string          `json:"base_url"`
	Logo           string          `json:"logo"`
	Avatar         string          `json:"avatar"`
	UTCOffset      string          `json:"utc_offset"`
	HomePostsCount int             `json:"home_posts_count"`
	Nav            []restSiteNav   `json:"nav"`
	Features       restSiteFeature `json:"features"`
}

type restSiteNav struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type restSiteFeature struct {
	Categories bool `json:"categories"`
	Tags       bool `json:"tags"`
	Archives   bool `json:"archives"`
	Search     bool `json:"search"`
}

func (s *Server) restIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"version": "v1",
		"endpoints": []string{
			"GET /rest/v1/site",
			"GET /rest/v1/categories",
			"GET /rest/v1/tags",
			"GET /rest/v1/archives",
			"GET /rest/v1/posts",
			"GET /rest/v1/posts/{slug}",
			"GET /rest/v1/pages",
			"GET /rest/v1/pages/{slug}",
			"GET /rest/v1/search?q={keyword}",
		},
	})
}

func (s *Server) restSite(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, restSiteResponse{
		Name:           s.cfg.Site.Name,
		Description:    s.cfg.Site.Description,
		Author:         s.cfg.Site.Author,
		BaseURL:        s.cfg.Site.BaseURL,
		Logo:           s.cfg.Site.Logo,
		Avatar:         s.cfg.Site.AuthorAvatar,
		UTCOffset:      s.cfg.Site.UTCOffset,
		HomePostsCount: s.cfg.Pagination.HomePostsCount,
		Nav: []restSiteNav{
			{Name: "首页", URL: "/"},
			{Name: "全部篇章", URL: "/writings"},
			{Name: "分类", URL: "/categories"},
			{Name: "标签", URL: "/tags"},
			{Name: "归档", URL: "/archives"},
		},
		Features: restSiteFeature{Categories: true, Tags: true, Archives: true, Search: true},
	})
}

func (s *Server) restCategories(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.publicRESTPosts()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load categories")
		return
	}

	counts := make(map[string]int)
	uncategorized := 0
	for _, post := range posts {
		name := strings.TrimSpace(post.Category)
		if name == "" {
			uncategorized++
			continue
		}
		counts[name]++
	}
	items := restCategoryItems(counts, "/category/")
	if len(posts) > 0 {
		items = append([]restCategory{{Name: categoryNoneName, Count: uncategorized, URL: "/category/" + categoryNone, None: true}}, items...)
	}
	writeJSON(w, restCategoryListResponse{Items: items, Total: len(items)})
}

func (s *Server) restTags(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.publicRESTPosts()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load tags")
		return
	}

	counts := make(map[string]int)
	for _, post := range posts {
		for _, tag := range post.Tags {
			counts[tag]++
		}
	}
	writeJSON(w, restCategoryListResponse{Items: restCategoryItems(counts, "/tag/"), Total: len(counts)})
}

func (s *Server) restArchives(w http.ResponseWriter, _ *http.Request) {
	posts, err := s.publicRESTPosts()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load archives")
		return
	}

	counts := make(map[string]int)
	for _, post := range posts {
		counts[post.Date.Format("2006")]++
	}
	items := make([]restArchive, 0, len(counts))
	for year, count := range counts {
		items = append(items, restArchive{Year: year, Count: count, URL: "/archive/" + year})
	}
	slices.SortFunc(items, func(a, b restArchive) int {
		return strings.Compare(b.Year, a.Year)
	})
	writeJSON(w, restArchiveListResponse{Items: items, Total: len(items)})
}

func restCategoryItems(counts map[string]int, prefix string) []restCategory {
	items := make([]restCategory, 0, len(counts))
	for name, count := range counts {
		items = append(items, restCategory{Name: name, Count: count, URL: prefix + url.PathEscape(name)})
	}
	slices.SortFunc(items, func(a, b restCategory) int {
		return strings.Compare(a.Name, b.Name)
	})
	return items
}

func (s *Server) restPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := s.publicRESTPosts()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load posts")
		return
	}

	category := strings.TrimSpace(r.URL.Query().Get("category"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	year := strings.TrimSpace(r.URL.Query().Get("year"))
	if year != "" && !restYear(year) {
		restError(w, http.StatusBadRequest, "year must be a four-digit year")
		return
	}
	if category != "" || tag != "" || year != "" {
		filtered := make([]*content.Item, 0, len(posts))
		for _, post := range posts {
			if category == categoryNone {
				if strings.TrimSpace(post.Category) != "" {
					continue
				}
			} else if category != "" && post.Category != category {
				continue
			}
			if tag != "" && !slices.Contains(post.Tags, tag) {
				continue
			}
			if year != "" && post.Date.Format("2006") != year {
				continue
			}
			filtered = append(filtered, post)
		}
		posts = filtered
	}

	page, perPage := restPagination(r)
	pageItems, totalPages := restPage(posts, page, perPage)
	items := make([]restContent, 0, len(pageItems))
	for _, item := range pageItems {
		items = append(items, restContentFromItem(item, false, false))
	}
	writeJSON(w, restListResponse{Items: items, Page: page, PerPage: perPage, Total: len(posts), TotalPages: totalPages})
}

func (s *Server) restPostDetail(w http.ResponseWriter, r *http.Request) {
	slug, item, ok := restItemForDetail(w, r, s, content.TypePost, "/rest/v1/posts/")
	if !ok {
		return
	}
	posts, err := s.publicRESTPosts()
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load posts")
		return
	}

	result := restContentFromItem(item, true, false)
	result.Canonical = frontendAbsoluteURL(s.cfg.Site.BaseURL, restPublicURL(item))
	for i, post := range posts {
		if post.Slug != slug {
			continue
		}
		if i > 0 {
			result.Next = restPostLinkFromItem(posts[i-1])
		}
		if i+1 < len(posts) {
			result.Previous = restPostLinkFromItem(posts[i+1])
		}
		break
	}
	writeJSON(w, result)
}

func (s *Server) publicRESTPosts() ([]*content.Item, error) {
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		return nil, err
	}
	posts = publicPosts(posts)
	sortPublic(posts)
	return posts, nil
}

func restPostLinkFromItem(item *content.Item) *restPostLink {
	return &restPostLink{Title: item.Title, Slug: item.Slug, URL: restPublicURL(item)}
}

func (s *Server) restPages(w http.ResponseWriter, r *http.Request) {
	pages, err := s.repo.List(content.TypePage)
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to load pages")
		return
	}
	pages = publicPosts(pages)
	sortPublic(pages)

	page, perPage := restPagination(r)
	pageItems, totalPages := restPage(pages, page, perPage)
	items := make([]restContent, 0, len(pageItems))
	for _, item := range pageItems {
		items = append(items, restContentFromItem(item, false, false))
	}
	writeJSON(w, restListResponse{Items: items, Page: page, PerPage: perPage, Total: len(pages), TotalPages: totalPages})
}

func (s *Server) restPageDetail(w http.ResponseWriter, r *http.Request) {
	_, item, ok := restItemForDetail(w, r, s, content.TypePage, "/rest/v1/pages/")
	if !ok {
		return
	}
	writeJSON(w, restContentFromItem(item, true, false))
}

func restItemForDetail(w http.ResponseWriter, r *http.Request, s *Server, typ content.Type, prefix string) (string, *content.Item, bool) {
	slug, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil || slug == "" || strings.Contains(slug, "/") {
		restError(w, http.StatusNotFound, "content not found")
		return "", nil, false
	}
	item, err := s.repo.GetBySlug(slug, typ)
	if err != nil || !item.Publish {
		restError(w, http.StatusNotFound, "content not found")
		return "", nil, false
	}
	return slug, item, true
}

func (s *Server) restSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		restError(w, http.StatusBadRequest, "q is required")
		return
	}
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		restError(w, http.StatusInternalServerError, "failed to search posts")
		return
	}
	posts = search.PublicPosts(posts, query)
	sortPublic(posts)

	page, perPage := restPagination(r)
	pageItems, totalPages := restPage(posts, page, perPage)
	items := make([]restContent, 0, len(pageItems))
	for _, item := range pageItems {
		items = append(items, restContentFromItem(item, false, true))
	}
	writeJSON(w, restListResponse{Items: items, Page: page, PerPage: perPage, Total: len(posts), TotalPages: totalPages})
}

func restContentFromItem(item *content.Item, detail, searchResult bool) restContent {
	result := restContent{
		Title:       item.Title,
		Description: item.Description,
		Date:        item.Date,
		Lastmod:     item.Lastmod,
		Slug:        item.Slug,
		Category:    item.Category,
		Tags:        item.Tags,
		CoverImage:  item.CoverImage,
		TOC:         item.TOC,
		URL:         restPublicURL(item),
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
	return result
}

func restPublicURL(item *content.Item) string {
	if item.Type == content.TypePage {
		return "/page/" + url.PathEscape(item.Slug)
	}
	return "/post/" + url.PathEscape(item.Slug)
}

func restPagination(r *http.Request) (int, int) {
	page := restPositiveInt(r.URL.Query().Get("page"), 1)
	perPage := restPositiveInt(r.URL.Query().Get("per_page"), restDefaultPerPage)
	perPage = min(perPage, restMaxPerPage)
	return page, perPage
}

func restPositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func restYear(value string) bool {
	if len(value) != 4 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func restPage[T any](items []T, page, perPage int) ([]T, int) {
	totalPages := (len(items) + perPage - 1) / perPage
	start := (page - 1) * perPage
	if start >= len(items) {
		return []T{}, totalPages
	}
	end := min(start+perPage, len(items))
	return items[start:end], totalPages
}

func restError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}
