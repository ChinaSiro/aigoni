package server

import (
	"sort"

	"aigoni/internal/content"
)

func publicPosts(items []*content.Item) []*content.Item {
	out := make([]*content.Item, 0, len(items))
	for _, item := range items {
		if item.Publish {
			out = append(out, item)
		}
	}
	return out
}

func sortPublic(items []*content.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		return a.Date.After(b.Date)
	})
}
