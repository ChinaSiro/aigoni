package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRESTIndexListsEndpointsWithoutAPIKey(t *testing.T) {
	srv := testServer(t)
	res := performRESTRequest(t, srv, "/rest/v1")

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "/rest/v1/site") || !strings.Contains(res.Body.String(), "/rest/v1/archives") {
		t.Fatalf("index does not list P1 endpoints: %s", res.Body.String())
	}
}

func TestRESTSiteOnlyReturnsPublicConfiguration(t *testing.T) {
	srv := testServer(t)
	srv.cfg.Site.Description = "A site"
	srv.cfg.Site.Author = "Author"
	srv.cfg.Site.BaseURL = "https://example.com"
	srv.cfg.Site.Logo = "/uploads/site/logo.png"
	srv.cfg.Site.AuthorAvatar = "/uploads/site/avatar.png"
	srv.cfg.Site.UTCOffset = "+08:00"
	srv.cfg.Pagination.HomePostsCount = 5

	res := performRESTRequest(t, srv, "/rest/v1/site")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restSiteResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Name != "Aigoni" || response.Author != "Author" || response.BaseURL != "https://example.com" || response.Avatar != "/uploads/site/avatar.png" || response.HomePostsCount != 5 {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Nav) == 0 || !response.Features.Tags || !response.Features.Archives || !response.Features.Search {
		t.Fatalf("response missing public navigation or features: %+v", response)
	}
	if strings.Contains(res.Body.String(), "secret") || strings.Contains(res.Body.String(), "admin") {
		t.Fatalf("site response exposed private configuration: %s", res.Body.String())
	}
}

func TestRESTTagsAndArchivesOnlyCountPublishedPosts(t *testing.T) {
	srv := testServer(t)
	writeRESTPostWithTags(t, srv, "2025-01-02-2", "Published", "published", "Go", []string{"Go", "API"}, true, "Body")
	writeRESTPostWithTags(t, srv, "2025-01-03-3", "Draft", "draft", "Go", []string{"Go", "Private"}, false, "Body")

	tags := performRESTRequest(t, srv, "/rest/v1/tags")
	if tags.Code != http.StatusOK {
		t.Fatalf("tags status = %d, body = %s", tags.Code, tags.Body.String())
	}
	var tagResponse restCategoryListResponse
	if err := json.NewDecoder(tags.Body).Decode(&tagResponse); err != nil {
		t.Fatal(err)
	}
	if tagResponse.Total != 2 || tagResponse.Items[0].Name != "API" || tagResponse.Items[0].Count != 1 || tagResponse.Items[0].URL != "/tag/API" {
		t.Fatalf("tag response = %+v", tagResponse)
	}

	archives := performRESTRequest(t, srv, "/rest/v1/archives")
	if archives.Code != http.StatusOK {
		t.Fatalf("archives status = %d, body = %s", archives.Code, archives.Body.String())
	}
	var archiveResponse restArchiveListResponse
	if err := json.NewDecoder(archives.Body).Decode(&archiveResponse); err != nil {
		t.Fatal(err)
	}
	if archiveResponse.Total != 2 || archiveResponse.Items[0].Year != "2025" || archiveResponse.Items[0].Count != 1 || archiveResponse.Items[0].URL != "/archive/2025" {
		t.Fatalf("archive response = %+v", archiveResponse)
	}
}

func TestRESTPostsOnlyReturnsPublishedPosts(t *testing.T) {
	srv := testServer(t)
	writeFile(t, filepath.Join(srv.root, "content/posts/2024/2024-01-02-2.md"), `---
title: "Draft"
description: "Hidden"
date: "2024-01-02T10:00:00Z"
publish: false
slug: "draft"
category: "Private"
tags: []
---

Secret body
`)

	res := performRESTRequest(t, srv, "/rest/v1/posts")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Items) != 1 || response.Items[0].Slug != "hello" {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].Body != "" || response.Items[0].HTML != "" {
		t.Fatalf("list exposed full content: %+v", response.Items[0])
	}
}

func TestRESTPostsFiltersCategoryAndPaginates(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Go One", "go-one", "Go", true, "Go keyword")
	writeRESTPost(t, srv, "2024-01-03-3", "Go Two", "go-two", "Go", true, "More Go")
	writeRESTPost(t, srv, "2024-01-04-4", "Rust", "rust", "Rust", true, "Rust body")
	writeRESTPost(t, srv, "2024-01-05-5", "No Category", "no-category", "", true, "No category body")

	res := performRESTRequest(t, srv, "/rest/v1/posts?category=Go&page=2&per_page=1")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || response.TotalPages != 2 || response.Page != 2 || len(response.Items) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].Slug != "go-one" {
		t.Fatalf("slug = %q, want go-one", response.Items[0].Slug)
	}

	none := performRESTRequest(t, srv, "/rest/v1/posts?category=__none__")
	if none.Code != http.StatusOK {
		t.Fatalf("none status = %d, body = %s", none.Code, none.Body.String())
	}
	var noneResponse restListResponse
	if err := json.NewDecoder(none.Body).Decode(&noneResponse); err != nil {
		t.Fatal(err)
	}
	if noneResponse.Total != 2 || len(noneResponse.Items) != 2 || noneResponse.Items[0].Slug != "no-category" || noneResponse.Items[1].Slug != "hello" {
		t.Fatalf("none response = %+v", noneResponse)
	}
}

func TestRESTPostsFiltersTagAndYear(t *testing.T) {
	srv := testServer(t)
	writeRESTPostWithTags(t, srv, "2025-01-02-2", "Go API", "go-api", "Go", []string{"Go", "API"}, true, "Body")
	writeRESTPostWithTags(t, srv, "2025-01-03-3", "Go 2024", "go-2024", "Go", []string{"Go"}, true, "Body")
	writeRESTPostWithTags(t, srv, "2024-01-04-4", "API 2024", "api-2024", "API", []string{"API"}, true, "Body")

	res := performRESTRequest(t, srv, "/rest/v1/posts?tag=API&year=2025")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Items) != 1 || response.Items[0].Slug != "go-api" {
		t.Fatalf("response = %+v", response)
	}

	invalid := performRESTRequest(t, srv, "/rest/v1/posts?year=202")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid year status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestRESTPostDetailReturnsBodyHTMLNavigationAndCanonical(t *testing.T) {
	srv := testServer(t)
	srv.cfg.Site.BaseURL = "https://example.com/"
	writeRESTPost(t, srv, "2024-01-02-2", "Newer", "newer", "Go", true, "Newer body")
	writeRESTPost(t, srv, "2023-01-02-3", "Older", "older", "Go", true, "Older body")
	res := performRESTRequest(t, srv, "/rest/v1/posts/hello")

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restContent
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body, "Body") || !strings.Contains(response.HTML, "Body") {
		t.Fatalf("response = %+v", response)
	}
	if response.Canonical != "https://example.com/post/hello" {
		t.Fatalf("canonical = %q", response.Canonical)
	}
	if response.Next == nil || response.Next.Slug != "newer" || response.Previous == nil || response.Previous.Slug != "older" {
		t.Fatalf("navigation = previous:%+v next:%+v", response.Previous, response.Next)
	}
}

func TestRESTPostDetailHidesDraft(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Draft", "draft", "Private", false, "Secret")

	res := performRESTRequest(t, srv, "/rest/v1/posts/draft")
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestRESTCategoriesOnlyCountsPublishedPosts(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Public", "public", "Go", true, "Body")
	writeRESTPost(t, srv, "2024-01-03-3", "Draft", "draft", "Go", false, "Body")
	writeRESTPost(t, srv, "2024-01-04-4", "Uncategorized", "uncategorized", "", true, "Body")
	writeRESTPost(t, srv, "2024-01-05-5", "Draft None", "draft-none", "", false, "Body")

	res := performRESTRequest(t, srv, "/rest/v1/categories")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restCategoryListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || len(response.Items) != 2 {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].Name != categoryNoneName || response.Items[0].Count != 2 || response.Items[0].URL != "/category/__none__" || !response.Items[0].None {
		t.Fatalf("uncategorized item = %+v", response.Items[0])
	}
	if response.Items[1].Name != "Go" || response.Items[1].Count != 1 {
		t.Fatalf("category item = %+v", response.Items[1])
	}
}

func TestRESTCategoriesIncludesUncategorizedItemWithoutEmptyPosts(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-01-1", "Hello", "hello", "Base", true, "Body")
	writeRESTPost(t, srv, "2024-01-02-2", "Public", "public", "Go", true, "Body")

	res := performRESTRequest(t, srv, "/rest/v1/categories")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restCategoryListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 3 || len(response.Items) != 3 {
		t.Fatalf("response = %+v", response)
	}
	if response.Items[0].Name != categoryNoneName || response.Items[0].Count != 0 || !response.Items[0].None {
		t.Fatalf("uncategorized item = %+v", response.Items[0])
	}
}

func TestRESTPagesOnlyReturnsPublishedPages(t *testing.T) {
	srv := testServer(t)
	writeRESTPage(t, srv, "2024-01-01-1", "About", "about", true)
	writeRESTPage(t, srv, "2024-01-02-2", "Draft Page", "draft-page", false)

	list := performRESTRequest(t, srv, "/rest/v1/pages")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var response restListResponse
	if err := json.NewDecoder(list.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || response.Items[0].Slug != "about" {
		t.Fatalf("response = %+v", response)
	}

	detail := performRESTRequest(t, srv, "/rest/v1/pages/about")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "About body") {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body.String())
	}

	draft := performRESTRequest(t, srv, "/rest/v1/pages/draft-page")
	if draft.Code != http.StatusNotFound {
		t.Fatalf("draft status = %d, body = %s", draft.Code, draft.Body.String())
	}
}

func TestRESTSearchRequiresQueryAndHidesDrafts(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Public Go", "public-go", "Go", true, "Unique keyword")
	writeRESTPost(t, srv, "2024-01-03-3", "Draft Go", "draft-go", "Go", false, "Unique keyword")

	missing := performRESTRequest(t, srv, "/rest/v1/search")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing query status = %d", missing.Code)
	}

	res := performRESTRequest(t, srv, "/rest/v1/search?q=Unique")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response restListResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || response.Items[0].Slug != "public-go" || response.Items[0].Excerpt == "" {
		t.Fatalf("response = %+v", response)
	}
}

func performRESTRequest(t *testing.T, srv *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}

func writeRESTPost(t *testing.T, srv *Server, id, title, slug, category string, publish bool, body string) {
	writeRESTPostWithTags(t, srv, id, title, slug, category, []string{"API"}, publish, body)
}

func writeRESTPostWithTags(t *testing.T, srv *Server, id, title, slug, category string, tags []string, publish bool, body string) {
	t.Helper()
	date := id
	if len(date) > len("2006-01-02") {
		date = date[:len("2006-01-02")]
	}
	writeFile(t, filepath.Join(srv.root, "content/posts/2024", id+".md"), `---
title: "`+title+`"
description: "Description"
date: "`+date+`T10:00:00Z"
publish: `+strconvFormatBool(publish)+`
slug: "`+slug+`"
category: "`+category+`"
tags: `+restTagsYAML(tags)+`
---

`+body+`
`)
}

func writeRESTPage(t *testing.T, srv *Server, id, title, slug string, publish bool) {
	t.Helper()
	writeFile(t, filepath.Join(srv.root, "content/pages/2024", id+".md"), `---
title: "`+title+`"
description: "Page description"
date: "2024-01-01T10:00:00Z"
publish: `+strconvFormatBool(publish)+`
slug: "`+slug+`"
tags: []
---

`+title+` body
`)
}

func restTagsYAML(tags []string) string {
	quoted := make([]string, 0, len(tags))
	for _, tag := range tags {
		quoted = append(quoted, strconv.Quote(tag))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func strconvFormatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
