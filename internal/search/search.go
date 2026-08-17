package search

import (
	"html"
	"strings"
	"unicode/utf8"

	"aigoni/internal/content"
)

func PublicPosts(items []*content.Item, query string) []*content.Item {
	return filter(items, query, true)
}

func All(items []*content.Item, query string) []*content.Item {
	return filter(items, query, false)
}

func filter(items []*content.Item, query string, publicOnly bool) []*content.Item {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var out []*content.Item
	for _, item := range items {
		if publicOnly && (item.Type != content.TypePost || !item.Publish) {
			continue
		}
		haystack := strings.ToLower(item.Title + " " + item.Description + " " + item.Body)
		if strings.Contains(haystack, query) {
			// 生成命中摘录
			excerpt := extractExcerpt(item, query)
			itemCopy := *item
			itemCopy.Excerpt = excerpt
			out = append(out, &itemCopy)
		}
	}
	return out
}

// extractExcerpt 提取命中摘录并高亮关键词
func extractExcerpt(item *content.Item, query string) string {
	queryLower := strings.ToLower(query)

	// 1. 尝试从正文中提取命中片段
	if excerpt := extractFromBody(item.Body, queryLower); excerpt != "" {
		return highlightKeyword(excerpt, query)
	}

	// 2. 如果标题命中，检查 description 是否也命中
	titleMatch := strings.Contains(strings.ToLower(item.Title), queryLower)
	if titleMatch && item.Description != "" && strings.Contains(strings.ToLower(item.Description), queryLower) {
		return highlightKeyword(item.Description, query)
	}

	// 3. 如果只有标题命中，返回原 description（不高亮）
	if titleMatch && item.Description != "" {
		return html.EscapeString(item.Description)
	}

	// 4. 如果 description 命中，返回高亮的 description
	if strings.Contains(strings.ToLower(item.Description), queryLower) {
		return highlightKeyword(item.Description, query)
	}

	return ""
}

// extractFromBody 从正文中提取包含关键词的片段
func extractFromBody(body, queryLower string) string {
	bodyLower := strings.ToLower(body)
	pos := strings.Index(bodyLower, queryLower)
	if pos == -1 {
		return ""
	}

	// 转为 rune 数组进行字符级别操作
	runes := []rune(body)
	runesLower := []rune(bodyLower)

	// 找到关键词在 rune 数组中的位置
	runeQuery := []rune(queryLower)
	queryPos := -1
	for i := 0; i <= len(runesLower)-len(runeQuery); i++ {
		match := true
		for j := 0; j < len(runeQuery); j++ {
			if runesLower[i+j] != runeQuery[j] {
				match = false
				break
			}
		}
		if match {
			queryPos = i
			break
		}
	}

	if queryPos == -1 {
		return ""
	}

	// 提取前后文，控制在120字符左右
	const maxLen = 120
	start := queryPos - 40
	if start < 0 {
		start = 0
	}
	end := queryPos + len(runeQuery) + 40
	if end > len(runes) {
		end = len(runes)
	}

	// 截取片段
	excerpt := string(runes[start:end])

	// 清理换行和多余空白
	excerpt = strings.ReplaceAll(excerpt, "\n", " ")
	excerpt = strings.ReplaceAll(excerpt, "\r", "")
	excerpt = strings.Join(strings.Fields(excerpt), " ")

	// 限制总长度
	if utf8.RuneCountInString(excerpt) > maxLen {
		excerptRunes := []rune(excerpt)
		excerpt = string(excerptRunes[:maxLen]) + "…"
	}

	// 如果不是从开头开始，加省略号
	if start > 0 {
		excerpt = "…" + excerpt
	}
	// 如果不是到结尾，加省略号
	if end < len(runes) && !strings.HasSuffix(excerpt, "…") {
		excerpt = excerpt + "…"
	}

	return excerpt
}

// highlightKeyword 高亮关键词
func highlightKeyword(text, query string) string {
	escaped := html.EscapeString(text)
	queryLower := strings.ToLower(query)
	escapedLower := strings.ToLower(escaped)

	// 查找所有匹配位置
	result := ""
	lastPos := 0
	for {
		pos := strings.Index(escapedLower[lastPos:], queryLower)
		if pos == -1 {
			result += escaped[lastPos:]
			break
		}
		actualPos := lastPos + pos
		result += escaped[lastPos:actualPos]
		matchedText := escaped[actualPos : actualPos+len(query)]
		result += `<mark class="search-highlight">` + matchedText + `</mark>`
		lastPos = actualPos + len(query)
	}

	return result
}
