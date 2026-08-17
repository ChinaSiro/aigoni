package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestValidateRemoteImageURLRejectsPrivateRanges(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/x.png",                  // 环回
		"http://localhost/x.png",                  // 单标签主机名
		"http://10.0.0.8/x.png",                   // RFC1918
		"http://172.16.0.8/x.png",                 // RFC1918
		"http://192.168.1.8/x.png",                // RFC1918
		"http://169.254.169.254/latest/meta-data", // 链路本地 + 云元数据
		"http://[::1]/x.png",                      // IPv6 环回
		"http://[fc00::1]/x.png",                  // IPv6 ULA
		"http://[fe80::1]/x.png",                  // IPv6 链路本地
		"http://100.100.100.200/x.png",            // 云元数据
		"http://100.64.0.1/x.png",                 // CGNAT
		"ftp://example.com/x.png",                 // 非 http(s)
		"not-a-url",
		"",
	}
	for _, raw := range blocked {
		u, err := url.Parse(raw)
		if err != nil && raw != "not-a-url" && raw != "" {
			// 非 URL 直接由 Parse 拒绝。
			continue
		}
		if validateRemoteImageURL(u) == nil {
			t.Errorf("validateRemoteImageURL(%q) accepted blocked URL", raw)
		}
	}

	// 合法公网主机名应通过（不解析到内网）。
	public, err := url.Parse("https://example.com/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRemoteImageURL(public); err != nil {
		t.Errorf("validateRemoteImageURL accepted public URL, err: %v", err)
	}
}

func TestRemoteImageRejectsLoopbackInitialURL(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	// remoteImageValidate 默认 true：初始 URL 是环回地址，应被拒绝。
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestRemoteImageRejectsNonImageResponse(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	srv.remoteImageValidate = false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}

func TestRemoteImageRejectsRedirectToPrivate(t *testing.T) {
	srv := testServer(t)
	srv.env.AigoniAPIKey = "test-key"
	// 关闭初始校验以在环回搭建测试服务器，但保持默认客户端的重定向校验。
	srv.remoteImageValidate = false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 重定向到环回地址，默认客户端 CheckRedirect 会拒绝。
		http.Redirect(w, r, "http://127.0.0.1:1/steal.png", http.StatusFound)
	}))
	defer remote.Close()

	body := `{"type":"note","body":"---\ntitle: X\ndate: 2026-08-10T00:00:00Z\n---\n\n![图](` + remote.URL + `/image.png)","sync_images":true}`
	res := performAPIRequest(t, srv, "/api/content", body)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", res.Code, res.Body.String())
	}
}
