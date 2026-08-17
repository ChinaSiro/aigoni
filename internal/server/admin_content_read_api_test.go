package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAPIReadOnlyContentEndpoints(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Draft", "draft", "Private", false, "Draft body")
	writeRESTPage(t, srv, "2024-01-03-3", "Private Page", "private-page", false)
	writeFile(t, filepath.Join(srv.root, "content/notes/2024/2024-01-02-2.md"), `---
title: "Indexed Note"
description: "Note description"
date: "2024-01-02T10:00:00Z"
category: "Research"
tags: ["private"]
---

Note body
`)
	cookie := adminAPICookie(t, srv)

	list := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/posts?publish=false")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var posts apiListResponse[adminContent]
	if err := json.NewDecoder(list.Body).Decode(&posts); err != nil {
		t.Fatal(err)
	}
	if posts.Total != 1 || len(posts.Items) != 1 || posts.Items[0].Slug != "draft" || posts.Items[0].Body != "" {
		t.Fatalf("posts = %#v", posts)
	}
	if posts.Items[0].Revision == "" || posts.Items[0].Path == "" || posts.Items[0].Type != "post" {
		t.Fatalf("list item lacks required fields: %#v", posts.Items[0])
	}

	detail := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/posts/2024/2024-01-02-2")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	var post adminContent
	if err := json.NewDecoder(detail.Body).Decode(&post); err != nil {
		t.Fatal(err)
	}
	if post.Body != "Draft body\n" || !strings.Contains(post.HTML, "Draft body") {
		t.Fatalf("detail = %#v", post)
	}
	data, err := os.ReadFile(post.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if post.Revision != hex.EncodeToString(sum[:]) {
		t.Fatalf("revision = %q", post.Revision)
	}

	notes := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/notes?category=Research")
	if notes.Code != http.StatusOK {
		t.Fatalf("notes status = %d, body = %s", notes.Code, notes.Body.String())
	}
	var noteList apiListResponse[adminContent]
	if err := json.NewDecoder(notes.Body).Decode(&noteList); err != nil {
		t.Fatal(err)
	}
	if noteList.Total != 1 || noteList.Items[0].Category != "Research" {
		t.Fatalf("notes = %#v", noteList)
	}

	tagFiltered := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/notes?category=Research&tag=private")
	if tagFiltered.Code != http.StatusOK {
		t.Fatalf("notes tag filter status = %d, body = %s", tagFiltered.Code, tagFiltered.Body.String())
	}
	var tagNoteList apiListResponse[adminContent]
	if err := json.NewDecoder(tagFiltered.Body).Decode(&tagNoteList); err != nil {
		t.Fatal(err)
	}
	if tagNoteList.Total != 1 {
		t.Fatalf("notes tag filter = %#v", tagNoteList)
	}

	tagMiss := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/notes?tag=nomatch")
	if tagMiss.Code != http.StatusOK {
		t.Fatalf("notes tag miss status = %d, body = %s", tagMiss.Code, tagMiss.Body.String())
	}
	var tagMissList apiListResponse[adminContent]
	if err := json.NewDecoder(tagMiss.Body).Decode(&tagMissList); err != nil {
		t.Fatal(err)
	}
	if tagMissList.Total != 0 {
		t.Fatalf("notes tag miss = %#v", tagMissList)
	}
}

func TestAdminAPIReadOnlyDashboardCategoriesAndSearch(t *testing.T) {
	srv := testServer(t)
	writeRESTPost(t, srv, "2024-01-02-2", "Go Post", "go-post", "Go", false, "Needle")
	writeFile(t, filepath.Join(srv.root, "content/notes/2024/2024-01-02-2.md"), `---
title: "Needle Note"
description: ""
date: "2024-01-02T10:00:00Z"
category: "Research"
tags: ["private"]
---

Needle body
`)
	cookie := adminAPICookie(t, srv)

	dashboard := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/dashboard")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), `"notes":2`) {
		t.Fatalf("dashboard status = %d, body = %s", dashboard.Code, dashboard.Body.String())
	}

	categories := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/categories")
	if categories.Code != http.StatusOK || !strings.Contains(categories.Body.String(), `"name":"Go"`) {
		t.Fatalf("categories status = %d, body = %s", categories.Code, categories.Body.String())
	}
	if !strings.Contains(categories.Body.String(), `"name":"未分类","count":1,"none":true`) {
		t.Fatalf("categories missing uncategorized item, body = %s", categories.Body.String())
	}
	noteCategories := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/note-categories")
	if noteCategories.Code != http.StatusOK || !strings.Contains(noteCategories.Body.String(), `"name":"Research"`) {
		t.Fatalf("note categories status = %d, body = %s", noteCategories.Code, noteCategories.Body.String())
	}
	if !strings.Contains(noteCategories.Body.String(), `"name":"未分类","count":1,"none":true`) {
		t.Fatalf("note categories missing uncategorized item, body = %s", noteCategories.Body.String())
	}

	tags := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/tags")
	if tags.Code != http.StatusOK || !strings.Contains(tags.Body.String(), `"name":"API"`) {
		t.Fatalf("tags status = %d, body = %s", tags.Code, tags.Body.String())
	}
	noteTags := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/note-tags")
	if noteTags.Code != http.StatusOK || !strings.Contains(noteTags.Body.String(), `"name":"private"`) {
		t.Fatalf("note tags status = %d, body = %s", noteTags.Code, noteTags.Body.String())
	}

	nonePosts := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/posts?category=__none__")
	if nonePosts.Code != http.StatusOK {
		t.Fatalf("posts none category status = %d, body = %s", nonePosts.Code, nonePosts.Body.String())
	}
	var nonePostList apiListResponse[adminContent]
	if err := json.NewDecoder(nonePosts.Body).Decode(&nonePostList); err != nil {
		t.Fatal(err)
	}
	if nonePostList.Total != 1 || nonePostList.Items[0].Slug != "hello" {
		t.Fatalf("posts none category = %#v", nonePostList)
	}

	noneNotes := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/notes?category=__none__")
	if noneNotes.Code != http.StatusOK {
		t.Fatalf("notes none category status = %d, body = %s", noneNotes.Code, noneNotes.Body.String())
	}
	var noneNoteList apiListResponse[adminContent]
	if err := json.NewDecoder(noneNotes.Body).Decode(&noneNoteList); err != nil {
		t.Fatal(err)
	}
	if noneNoteList.Total != 1 {
		t.Fatalf("notes none category = %#v", noneNoteList)
	}

	search := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/search?q=Needle")
	if search.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", search.Code, search.Body.String())
	}
	var results apiListResponse[adminContent]
	if err := json.NewDecoder(search.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	if results.Total != 2 || results.Items[0].Excerpt == "" || results.Items[1].EditURL == "" {
		t.Fatalf("search = %#v", results)
	}

	invalidType := performAdminAPIRequest(t, srv, cookie, "/api/admin/v1/search?q=Needle&type=invalid")
	assertAdminAPIError(t, invalidType, http.StatusBadRequest, "validation_failed")
}

func adminAPICookie(t *testing.T, srv *Server) *http.Cookie {
	t.Helper()
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/session", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(login, req)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	return login.Result().Cookies()[0]
}

func performAdminAPIRequest(t *testing.T, srv *Server, cookie *http.Cookie, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, req)
	return response
}
