package search

import (
	"testing"

	"aigoni/internal/content"
)

func TestPublicPostsOnlySearchesPublishedPosts(t *testing.T) {
	items := []*content.Item{
		{Type: content.TypePost, Publish: true, Title: "Visible"},
		{Type: content.TypePost, Publish: false, Title: "Hidden"},
		{Type: content.TypeNote, Title: "Visible private"},
	}
	got := PublicPosts(items, "visible")
	if len(got) != 1 || got[0].Title != "Visible" {
		t.Fatalf("PublicPosts returned %#v, want only visible post", got)
	}
}
