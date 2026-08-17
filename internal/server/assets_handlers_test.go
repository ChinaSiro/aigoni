package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadsRejectDirectoryListingAndSetNosniff(t *testing.T) {
	srv := testServer(t)
	uploadsDir := filepath.Join(srv.root, "public", "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "logo.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := srv.Handler()

	// 目录请求必须 404，不做目录列表。
	dirRec := httptest.NewRecorder()
	handler.ServeHTTP(dirRec, httptest.NewRequest(http.MethodGet, "/uploads/", nil))
	if dirRec.Code != http.StatusNotFound {
		t.Fatalf("dir status = %d, body = %s, want 404", dirRec.Code, dirRec.Body.String())
	}

	// 文件请求 200 且带 nosniff。
	fileRec := httptest.NewRecorder()
	handler.ServeHTTP(fileRec, httptest.NewRequest(http.MethodGet, "/uploads/logo.png", nil))
	if fileRec.Code != http.StatusOK {
		t.Fatalf("file status = %d, want 200", fileRec.Code)
	}
	if fileRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options: nosniff")
	}
}

func TestAssetsRejectDirectoryListingAndSetNosniff(t *testing.T) {
	srv := testServer(t)
	assetDir := filepath.Join(srv.root, "content", "posts", "2024", "2024-01-01-1.assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "image.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := srv.Handler()

	dirRec := httptest.NewRecorder()
	handler.ServeHTTP(dirRec, httptest.NewRequest(http.MethodGet, "/assets/posts/2024/2024-01-01-1.assets/", nil))
	if dirRec.Code != http.StatusNotFound {
		t.Fatalf("dir status = %d, want 404", dirRec.Code)
	}

	fileRec := httptest.NewRecorder()
	handler.ServeHTTP(fileRec, httptest.NewRequest(http.MethodGet, "/assets/posts/2024/2024-01-01-1.assets/image.png", nil))
	if fileRec.Code != http.StatusOK {
		t.Fatalf("file status = %d, want 200", fileRec.Code)
	}
	if fileRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options: nosniff")
	}
}
