package content

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsePostRequiresSlug(t *testing.T) {
	data := []byte(`---
title: "Post"
description: "Desc"
date: "2024-01-01T10:00:00Z"
publish: true
---

Body
`)
	if _, err := Parse(data, TypePost); err == nil {
		t.Fatal("Parse succeeded, want slug error")
	}
}

func TestParseTrimsSeparatorBlankLineFromBody(t *testing.T) {
	data := []byte(`---
title: "Post"
date: "2024-01-01T10:00:00Z"
publish: true
slug: "post"
---

Body
`)
	item, err := Parse(data, TypePost)
	if err != nil {
		t.Fatal(err)
	}
	if item.Body != "Body\n" {
		t.Fatalf("Body = %q, want %q", item.Body, "Body\n")
	}
}

func TestParseAcceptsFriendlyDateFormats(t *testing.T) {
	cases := []struct {
		name string
		date string
		hour int
		min  int
	}{
		{name: "date only", date: "2024-01-01", hour: 0, min: 0},
		{name: "minute precision", date: "2024-01-01 10:30", hour: 10, min: 30},
		{name: "second precision", date: "2024-01-01 10:30:45", hour: 10, min: 30},
		{name: "rfc3339", date: "2024-01-01T10:30:45Z", hour: 10, min: 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("---\ntitle: Post\ndate: \"" + tc.date + "\"\npublish: true\nslug: post\n---\n\nBody\n")
			item, err := Parse(data, TypePost)
			if err != nil {
				t.Fatal(err)
			}
			if item.Date.Hour() != tc.hour || item.Date.Minute() != tc.min {
				t.Fatalf("date = %s", item.Date)
			}
		})
	}
}

func TestAtomicWriteFileReplacesCompleteContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Fatalf("content = %q, want new content", data)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".aigoni-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, err = %v", matches, err)
	}
}

func TestRepositorySaveAndDeleteRemovesAssets(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	item, err := repo.Save(SaveInput{
		Type:    TypePost,
		Title:   "Hello",
		Date:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Publish: true,
		Slug:    "hello",
		Body:    "Body",
	})
	if err != nil {
		t.Fatal(err)
	}
	assetDir := filepath.Join(filepath.Dir(item.Path), "2024-01-01-1.assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "cover.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(item.ID, TypePost); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(assetDir); !os.IsNotExist(err) {
		t.Fatalf("asset dir still exists or stat failed: %v", err)
	}
}

func TestNewNoteDraftDefaultTitle(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	item, err := repo.NewNoteDraft(now)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "未命名笔记" {
		t.Fatalf("Title = %q, want 未命名笔记", item.Title)
	}
	want := "2026/2026-06-18-1-未命名笔记"
	if item.ID != want {
		t.Fatalf("ID = %q, want %q", item.ID, want)
	}
}

func TestSaveNoteRenamesTitleAndKeepsStablePrefix(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	note, err := repo.Save(SaveInput{Type: TypeNote, Title: "旧标题", Date: now, Body: "![image](/assets/notes/2026/2026-06-18-1-旧标题.assets/image.png)"})
	if err != nil {
		t.Fatal(err)
	}
	if note.ID != "2026/2026-06-18-1-旧标题" {
		t.Fatalf("initial ID = %q, want title suffix", note.ID)
	}
	oldPath := note.Path
	oldAssets := strings.TrimSuffix(oldPath, ".md") + ".assets"
	if err := os.MkdirAll(oldAssets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldAssets, "image.png"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved, err := repo.Save(SaveInput{
		ID: note.ID, Type: TypeNote, Title: "123啊啊334", Date: now,
		Body: note.Body,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantID := "2026/2026-06-18-1-123啊啊334"
	if saved.ID != wantID || saved.Path == oldPath {
		t.Fatalf("renamed note = ID %q, path %q; want %q and a new path", saved.ID, saved.Path, wantID)
	}
	if content, err := os.ReadFile(saved.Path); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(content), "2026-06-18-1-123啊啊334.assets/image.png") {
		t.Fatalf("saved body did not update asset path: %s", content)
	}
	newAssets := strings.TrimSuffix(saved.Path, ".md") + ".assets"
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old note path still exists: %v", err)
	}
	if _, err := os.Stat(oldAssets); !os.IsNotExist(err) {
		t.Fatalf("old assets path still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newAssets, "image.png")); err != nil {
		t.Fatalf("new assets path missing: %v", err)
	}
	if got := StableID(saved.ID, TypeNote); got != "2026/2026-06-18-1" {
		t.Fatalf("stable ID = %q", got)
	}
}

func TestLegacyNoteStablePrefixResolvesOriginalPath(t *testing.T) {
	dir := t.TempDir()
	notesDir := filepath.Join(dir, "notes")
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), notesDir)
	path := filepath.Join(notesDir, "2026", "2026-06-25-8-outlook注册机验证码的加速思路-5xejhi.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`---
title: "Legacy note"
date: "2026-06-25T10:00:00Z"
---

Body
`), 0o644); err != nil {
		t.Fatal(err)
	}

	item, err := repo.GetByID("2026/2026-06-25-8", TypeNote)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "2026/2026-06-25-8-outlook注册机验证码的加速思路-5xejhi" {
		t.Fatalf("resolved ID = %q", item.ID)
	}
	if StableID(item.ID, TypeNote) != "2026/2026-06-25-8" {
		t.Fatalf("stable ID = %q", StableID(item.ID, TypeNote))
	}
	if err := repo.Delete("2026/2026-06-25-8", TypeNote); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy note still exists: %v", err)
	}
}

func TestNoteFrontMatterWritesCategoryWikiStatusAndWikiHash(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	item, err := repo.Save(SaveInput{
		Type:       TypeNote,
		Title:      "Meta Note",
		Date:       now,
		Category:   "网页裁切",
		SourceURL:  "https://example.com/a",
		WikiStatus: "indexed",
		WikiHash:   "sha256:test",
		Tags:       []string{"ai"},
		Body:       "Body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Category != "网页裁切" {
		t.Fatalf("Category = %q, want 网页裁切", item.Category)
	}
	if item.WikiStatus != "indexed" {
		t.Fatalf("WikiStatus = %q, want indexed", item.WikiStatus)
	}
	data, err := os.ReadFile(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"category: 网页裁切", "wiki_status: indexed", "wiki_hash: sha256:test", "source_url: https://example.com/a"} {
		if !strings.Contains(body, want) {
			t.Fatalf("note frontmatter missing %q in:\n%s", want, body)
		}
	}
}

func TestConcurrentNoteCreationUsesUniqueNumericIDs(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	const count = 20
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			item, err := repo.Save(SaveInput{
				Type:  TypeNote,
				Title: fmt.Sprintf("并发笔记-%d", i),
				Date:  now,
				Body:  "Body",
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- item.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[int]bool, count)
	for id := range ids {
		n := seqFromID(id)
		if seen[n] {
			t.Fatalf("duplicate numeric ID %d in %q", n, id)
		}
		seen[n] = true
	}
	if len(seen) != count {
		t.Fatalf("created %d unique IDs, want %d", len(seen), count)
	}
	for i := 1; i <= count; i++ {
		if !seen[i] {
			t.Fatalf("missing numeric ID %d", i)
		}
	}
}

func TestConcurrentPostCreationUsesUniqueNumericIDs(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	const count = 20
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := repo.NewPostDraft(now)
			if err != nil {
				errs <- err
				return
			}
			ids <- item.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[int]bool, count)
	for id := range ids {
		n := seqFromID(id)
		if seen[n] {
			t.Fatalf("duplicate post numeric ID %d in %q", n, id)
		}
		seen[n] = true
	}
	if len(seen) != count {
		t.Fatalf("created %d unique post IDs, want %d", len(seen), count)
	}
}

func TestConcurrentPageCreationUsesUniqueNumericIDs(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	const count = 20
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := repo.NewPageDraft(now)
			if err != nil {
				errs <- err
				return
			}
			ids <- item.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[int]bool, count)
	for id := range ids {
		n := seqFromID(id)
		if seen[n] {
			t.Fatalf("duplicate page numeric ID %d in %q", n, id)
		}
		seen[n] = true
	}
	if len(seen) != count {
		t.Fatalf("created %d unique page IDs, want %d", len(seen), count)
	}
}

func TestNoteCreationDoesNotReuseDeletedNumericID(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

	first, err := repo.Save(SaveInput{Type: TypeNote, Title: "一", Date: now})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Save(SaveInput{Type: TypeNote, Title: "二", Date: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(first.ID, TypeNote); err != nil {
		t.Fatal(err)
	}
	third, err := repo.Save(SaveInput{Type: TypeNote, Title: "三", Date: now})
	if err != nil {
		t.Fatal(err)
	}
	if seqFromID(second.ID) != 2 || seqFromID(third.ID) != 3 {
		t.Fatalf("IDs after delete = %q, %q; want numeric IDs 2 and 3", second.ID, third.ID)
	}
}
