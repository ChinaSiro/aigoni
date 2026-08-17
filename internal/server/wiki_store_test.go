package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteWikiFileNoTempLeftover(t *testing.T) {
	srv := testServer(t)
	path := "wiki/concepts/atomic-test.md"
	if err := srv.writeWikiFile(path, "# Test\n\ncontent"); err != nil {
		t.Fatal(err)
	}
	full, err := srv.fullWikiWritePath(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "content") {
		t.Fatalf("data = %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(full))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".aigoni-wiki-") {
			t.Fatalf("leftover temp file: %s", entry.Name())
		}
	}
}

func TestReadWikiDocsSkipsTempFiles(t *testing.T) {
	srv := testServer(t)
	if err := srv.writeWikiFile("wiki/concepts/real.md", "# Real"); err != nil {
		t.Fatal(err)
	}
	// 手动放置一个原子写风格的隐藏临时文件，不应被当作正式页面。
	tmp := filepath.Join(srv.wikiRoot(), "concepts", ".aigoni-wiki-123.tmp")
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	docs, err := srv.readWikiDocs()
	if err != nil {
		t.Fatal(err)
	}
	foundReal, foundTemp := false, false
	for _, doc := range docs {
		if strings.Contains(doc.Path, ".aigoni-wiki-") {
			foundTemp = true
		}
		if doc.Path == "wiki/concepts/real.md" {
			foundReal = true
		}
	}
	if foundTemp {
		t.Fatal("temp file listed as wiki document")
	}
	if !foundReal {
		t.Fatalf("real doc missing, docs = %v", docs)
	}
}

func TestAtomicWriteWikiFileOverwritesAtomically(t *testing.T) {
	srv := testServer(t)
	path := "wiki/sources/source-test.md"
	if err := srv.writeWikiFile(path, "# v1"); err != nil {
		t.Fatal(err)
	}
	if err := srv.writeWikiFile(path, "# v2"); err != nil {
		t.Fatal(err)
	}
	full, err := srv.fullWikiWritePath(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "v2") {
		t.Fatalf("content not overwritten: %q", data)
	}
}
