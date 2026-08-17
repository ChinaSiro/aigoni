package server

import (
	"testing"

	"aigoni/internal/content"
)

func TestPaginate(t *testing.T) {
	items := make([]*content.Item, 25)
	for i := range items {
		items[i] = &content.Item{ID: string(rune('a' + i))}
	}

	// 第一页：前 20 条，有下一页无上一页。
	page1, extra := paginate(items, 20, 1, "/admin/posts")
	if len(page1) != 20 {
		t.Fatalf("page1 len = %d, want 20", len(page1))
	}
	if extra["Page"] != 1 || extra["TotalPages"] != 2 || extra["TotalCount"] != 25 {
		t.Fatalf("page1 extra = %#v", extra)
	}
	if extra["HasPrev"] != false || extra["HasNext"] != true {
		t.Fatalf("page1 nav flags wrong: %#v", extra)
	}
	if extra["PrevURL"] != nil {
		t.Fatalf("page1 should have no PrevURL")
	}
	if extra["NextURL"] != "/admin/posts?page=2" {
		t.Fatalf("NextURL = %v", extra["NextURL"])
	}

	// 第二页：剩余 5 条，有上一页无下一页。
	page2, extra2 := paginate(items, 20, 2, "/admin/posts")
	if len(page2) != 5 {
		t.Fatalf("page2 len = %d, want 5", len(page2))
	}
	if extra2["HasPrev"] != true || extra2["HasNext"] != false {
		t.Fatalf("page2 nav flags wrong: %#v", extra2)
	}
	if extra2["PrevURL"] != "/admin/posts?page=1" {
		t.Fatalf("PrevURL = %v", extra2["PrevURL"])
	}
}

func TestPaginateClampsOutOfRange(t *testing.T) {
	items := make([]*content.Item, 5)
	// page=99 夹到唯一页，无翻页。
	_, extra := paginate(items, 20, 99, "/admin/notes")
	if extra["Page"] != 1 || extra["TotalPages"] != 1 {
		t.Fatalf("clamped extra = %#v", extra)
	}
	if extra["HasPrev"] != false || extra["HasNext"] != false {
		t.Fatalf("single page should have no nav: %#v", extra)
	}
}
