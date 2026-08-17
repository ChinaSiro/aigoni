package content

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"
)

// md 带扩展的 goldmark 实例：
// - Table：GFM 表格语法渲染为 <table>（默认实例会把表格当普通段落）。
// - TaskList：`- [x]` / `* [ ]` 任务列表渲染为带 checkbox 的 <li>。
var md = goldmark.New(goldmark.WithExtensions(extension.Table, extension.TaskList))

// sanitize 在 UGCPolicy 基础上放行表格与任务列表 checkbox 标签：UGCPolicy 白名单默认不含。
var sanitize = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "th", "td",
		"caption", "colgroup", "col")
	// 任务列表 checkbox：只放行 input[type=checkbox]，带 checked/disabled 两属性。
	p.AllowElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`(?i)^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	return p
}()

type matter struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Date        string   `yaml:"date"`
	Lastmod     string   `yaml:"lastmod"`
	Publish     *bool    `yaml:"publish"`
	Slug        string   `yaml:"slug"`
	Category    string   `yaml:"category"`
	Tags        []string `yaml:"tags"`
	CoverImage  string   `yaml:"cover_image"`
	TOC         bool     `yaml:"toc"`
	Template    string   `yaml:"template"`
	SourceURL   string   `yaml:"source_url"`
	WikiStatus  string   `yaml:"wiki_status"`
	WikiHash    string   `yaml:"wiki_hash"`
	Weight      int      `yaml:"weight"`
}

// noteMatter 笔记专用元信息：只写笔记需要的字段，空值通过 omitempty 不落盘。
// 笔记与文章/独立页面规则不同，不写 publish/slug/cover_image/toc/template/weight。
type noteMatter struct {
	Title       string   `yaml:"title,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Date        string   `yaml:"date"`
	Lastmod     string   `yaml:"lastmod"`
	Category    string   `yaml:"category,omitempty"`
	SourceURL   string   `yaml:"source_url,omitempty"`
	WikiStatus  string   `yaml:"wiki_status,omitempty"`
	WikiHash    string   `yaml:"wiki_hash,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

func Parse(data []byte, typ Type) (*Item, error) {
	fm, body, err := splitFrontMatter(string(data))
	if err != nil {
		return nil, err
	}
	var meta matter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, fmt.Errorf("front matter parse failed: %w", err)
	}
	item, err := buildItem(meta, body, typ)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func RenderMarkdown(markdown string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}
	return sanitize.Sanitize(buf.String()), nil
}

func FrontMatter(input SaveInput) ([]byte, error) {
	if input.Title == "" {
		return nil, errors.New("title is required")
	}
	if input.Date.IsZero() {
		input.Date = time.Now().UTC()
	}
	meta := matter{
		Title:       input.Title,
		Description: input.Description,
		Date:        input.Date.UTC().Format(time.RFC3339),
		Lastmod:     time.Now().UTC().Format(time.RFC3339),
		Slug:        input.Slug,
		Category:    input.Category,
		Tags:        input.Tags,
		CoverImage:  input.CoverImage,
		TOC:         input.TOC,
		Template:    input.Template,
		SourceURL:   input.SourceURL,
		WikiStatus:  input.WikiStatus,
		WikiHash:    input.WikiHash,
		Weight:      input.Weight,
	}
	if input.Type != TypeNote {
		publish := input.Publish
		meta.Publish = &publish
	}
	data, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(data) + "---\n\n" + input.Body), nil
}

// NoteFrontMatter 笔记专用写入：只序列化笔记字段，空字段省略，title 可空。
func NoteFrontMatter(input SaveInput) ([]byte, error) {
	if input.Date.IsZero() {
		input.Date = time.Now().UTC()
	}
	meta := noteMatter{
		Title:       input.Title,
		Description: input.Description,
		Date:        input.Date.UTC().Format(time.RFC3339),
		Lastmod:     time.Now().UTC().Format(time.RFC3339),
		Category:    input.Category,
		SourceURL:   input.SourceURL,
		WikiStatus:  input.WikiStatus,
		WikiHash:    input.WikiHash,
		Tags:        input.Tags,
	}
	data, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(data) + "---\n\n" + input.Body), nil
}

func TemplateHTML(html string) template.HTML {
	return template.HTML(html)
}

func splitFrontMatter(data string) (string, string, error) {
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

func parseContentTime(field, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04", "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s must be RFC3339, YYYY-MM-DD, YYYY-MM-DD HH:MM, or YYYY-MM-DD HH:MM:SS", field)
}

func buildItem(meta matter, body string, typ Type) (*Item, error) {
	if typ != TypeNote && meta.Title == "" {
		return nil, errors.New("title is required")
	}
	if meta.Date == "" {
		return nil, errors.New("date is required")
	}
	date, err := parseContentTime("date", meta.Date)
	if err != nil {
		return nil, err
	}
	lastmod := date
	if meta.Lastmod != "" {
		lastmod, err = parseContentTime("lastmod", meta.Lastmod)
		if err != nil {
			return nil, err
		}
	}
	publish := true
	if typ != TypeNote {
		if meta.Publish == nil {
			return nil, errors.New("publish is required")
		}
		publish = *meta.Publish
	}
	if typ == TypePost && meta.Slug == "" {
		return nil, errors.New("slug is required")
	}
	if typ == TypePage && meta.Slug == "" {
		return nil, errors.New("slug is required")
	}
	html, err := RenderMarkdown(body)
	if err != nil {
		return nil, err
	}
	return &Item{
		Type:        typ,
		Title:       meta.Title,
		Description: meta.Description,
		Date:        date,
		Lastmod:     lastmod,
		Publish:     publish,
		Slug:        meta.Slug,
		Category:    meta.Category,
		Tags:        meta.Tags,
		CoverImage:  meta.CoverImage,
		TOC:         meta.TOC,
		Template:    meta.Template,
		SourceURL:   meta.SourceURL,
		WikiStatus:  meta.WikiStatus,
		WikiHash:    meta.WikiHash,
		Weight:      meta.Weight,
		Body:        body,
		HTML:        html,
	}, nil
}
