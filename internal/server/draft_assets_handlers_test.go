package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aigoni/internal/content"
)

func TestDraftAssetUploadAndCommit(t *testing.T) {
	srv := testServer(t)
	token := "0123456789abcdef0123456789abcdef"

	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("asset", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("png")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/admin/drafts/"+token+"/assets/upload", &uploadBody)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRes := httptest.NewRecorder()
	if !srv.draftAssets(uploadRes, uploadReq, []string{"drafts", token, "assets", "upload"}) {
		t.Fatal("draftAssets did not handle upload")
	}
	if uploadRes.Code != http.StatusOK {
		t.Fatalf("upload status = %d; body=%s", uploadRes.Code, uploadRes.Body.String())
	}
	draftPath := "/assets/.drafts/" + token + ".assets/image.png"
	if !strings.Contains(uploadRes.Body.String(), draftPath) {
		t.Fatalf("draft path missing: %s", uploadRes.Body.String())
	}

	item, err := srv.repo.Save(content.SaveInput{
		Type:  content.TypeNote,
		Title: "草稿迁移",
		Body:  "![图](" + draftPath + ")",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := srv.resourceService().CommitDraft(token, item, item.Body, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "/assets/.drafts/") || !strings.Contains(body, "/assets/notes/") {
		t.Fatalf("committed body = %q", body)
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSuffix(item.Path, ".md")+".assets", "image.png")); err != nil {
		t.Fatalf("committed asset missing: %v", err)
	}
}
