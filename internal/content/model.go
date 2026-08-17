package content

import "time"

type Type string

const (
	TypePost Type = "post"
	TypePage Type = "page"
	TypeNote Type = "note"
)

type Item struct {
	ID          string
	Type        Type
	Path        string
	Title       string
	Description string
	Date        time.Time
	Lastmod     time.Time
	Publish     bool
	Slug        string
	Category    string
	Tags        []string
	CoverImage  string
	TOC         bool
	Template    string
	SourceURL   string
	WikiStatus  string
	WikiHash    string
	Weight      int
	Body        string
	HTML        string
	Excerpt     string // 搜索命中摘录
}

type SaveInput struct {
	ID          string
	Type        Type
	Title       string
	Description string
	Date        time.Time
	Publish     bool
	Slug        string
	Category    string
	Tags        []string
	CoverImage  string
	TOC         bool
	Template    string
	SourceURL   string
	WikiStatus  string
	WikiHash    string
	Weight      int
	Body        string
}
