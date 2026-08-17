package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"aigoni/internal/content"
)

// sitemap 输出 sitemap.xml：首页、列表页（全部篇章/分类/标签/归档），
// 以及全部已发布文章与独立页面，供搜索引擎抓取收录。
func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.cfg.Site.BaseURL, "/")

	var urls []sitemapURL
	add := func(loc string, lm time.Time) {
		if loc == "" {
			return
		}
		if !strings.HasPrefix(loc, "http") {
			loc = base + loc
		}
		if lm.IsZero() {
			lm = time.Now().UTC()
		}
		urls = append(urls, sitemapURL{Loc: loc, Lastmod: lm})
	}

	add("/", time.Now().UTC())
	add("/writings", time.Now().UTC())

	if posts, err := s.repo.List(content.TypePost); err == nil {
		for _, p := range publicPosts(posts) {
			add("/post/"+p.Slug, latest(p.Date, p.Lastmod))
		}
	}
	if pages, err := s.repo.List(content.TypePage); err == nil {
		for _, p := range publicPosts(pages) {
			add("/page/"+p.Slug, latest(p.Date, p.Lastmod))
		}
	}

	if posts, err := s.repo.List(content.TypePost); err == nil {
		posts = publicPosts(posts)
		add("/categories", time.Now().UTC())
		add("/tags", time.Now().UTC())
		add("/archives", time.Now().UTC())

		cats := map[string]bool{}
		for _, p := range posts {
			if p.Category != "" {
				cats[p.Category] = true
			}
		}
		for c := range cats {
			add("/category/"+escapePath(c), time.Now().UTC())
		}
		tagSet := map[string]bool{}
		for _, p := range posts {
			for _, t := range p.Tags {
				tagSet[t] = true
			}
		}
		for t := range tagSet {
			add("/tag/"+escapePath(t), time.Now().UTC())
		}
		years := map[string]bool{}
		for _, p := range posts {
			years[p.Date.Format("2006")] = true
		}
		yearsList := make([]string, 0, len(years))
		for y := range years {
			yearsList = append(yearsList, y)
		}
		slices.Sort(yearsList)
		for _, y := range yearsList {
			add("/archive/"+y, time.Now().UTC())
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n")
	for _, u := range urls {
		fmt.Fprintf(w, "  <url>\n    <loc>%s</loc>\n    <lastmod>%s</lastmod>\n  </url>\n",
			xmlEscape(u.Loc), u.Lastmod.Format("2006-01-02"))
	}
	fmt.Fprint(w, "</urlset>\n")
}

type sitemapURL struct {
	Loc     string
	Lastmod time.Time
}

func latest(t ...time.Time) time.Time {
	out := time.Time{}
	for _, x := range t {
		if x.After(out) {
			out = x
		}
	}
	return out
}

// xmlEscape 转义 URL 中的 XML 特殊字符（& < >）。
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// escapePath 对路径段做百分号编码，仅用于 sitemap loc，
// 保证中文分类/标签符合 sitemap 协议（loc 需为合法 URL）。
func escapePath(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
