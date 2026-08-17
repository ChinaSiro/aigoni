package server

import (
	"fmt"
	"strconv"
	"strings"

	"aigoni/internal/content"
)

func (s *Server) filteredPosts(match func(*content.Item) bool) []*content.Item {
	posts, err := s.repo.List(content.TypePost)
	if err != nil {
		return nil
	}
	var out []*content.Item
	for _, item := range posts {
		if item.Publish && match(item) {
			out = append(out, item)
		}
	}
	sortPublic(out)
	return out
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func atoi(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func pageURL(baseURL string, page int) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%spage=%d", baseURL, sep, page)
}

// paginate 对列表做分页，返回当前页条目与模板分页元信息。
// perPage<1 视为 20；page 越界自动夹到有效区间。baseURL 形如 "/admin/posts"，
// PrevURL/NextURL 附加 page 参数。后台 posts/pages/notes 三列表共用。
func paginate(items []*content.Item, perPage, page int, baseURL string) ([]*content.Item, map[string]any) {
	if perPage < 1 {
		perPage = 20
	}
	total := len(items)
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	extra := map[string]any{
		"Page":       page,
		"TotalPages": totalPages,
		"TotalCount": total,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
	}
	if page > 1 {
		extra["PrevURL"] = pageURL(baseURL, page-1)
	}
	if page < totalPages {
		extra["NextURL"] = pageURL(baseURL, page+1)
	}
	return items[start:end], extra
}
