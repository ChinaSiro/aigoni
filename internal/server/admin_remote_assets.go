package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aigoni/internal/content"
)

var (
	errInvalidRemoteImageURL = errors.New("invalid remote image url")
	errRemoteImageTooLarge   = errors.New("remote image is too large")
	errRemoteImageNotImage   = errors.New("remote resource is not an image")
)

// blockedCIDRs 覆盖内网/保留/云元数据等不应被服务端访问的网段。
// net.IP 的 IsPrivate/IsLinkLocal 已覆盖多数场景，这里补充 CGNAT 与常见云元数据地址。
var blockedCIDRs = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),      // RFC 6598 CGNAT 共享地址
	netip.MustParsePrefix("100.100.100.200/32"), // 阿里云元数据
	netip.MustParsePrefix("100.100.100.204/32"),
}

// remoteImageHTTPClient 是远程图片下载专用客户端。
// CheckRedirect 在跟随每个重定向前重新校验目标，避免重定向把请求带回内网。
var remoteImageHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return validateRemoteImageURL(req.URL)
	},
}

func (s *Server) downloadRemoteImage(r *http.Request, id string, typ content.Type, rawURL, fallbackName string) (content.AssetFile, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return content.AssetFile{}, errInvalidRemoteImageURL
	}
	// 生产默认校验初始 URL；测试可关闭以使用环回测试服务器，但重定向校验始终生效。
	if s.remoteImageValidate {
		if err := validateRemoteImageURL(u); err != nil {
			return content.AssetFile{}, err
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return content.AssetFile{}, err
	}
	req.Header.Set("User-Agent", "Aigoni-Asset-Downloader/1.0")

	client := s.remoteImageClient
	if client == nil {
		client = remoteImageHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return content.AssetFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return content.AssetFile{}, fmt.Errorf("remote image download failed: %s", resp.Status)
	}
	// 只接受真正的图片响应，防止把 HTML/JSON/文本保存进资产目录。
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return content.AssetFile{}, errRemoteImageNotImage
	}
	if resp.ContentLength > remoteImageMaxBytes {
		return content.AssetFile{}, errRemoteImageTooLarge
	}

	name := remoteImageName(u, fallbackName, resp.Header.Get("Content-Type"))
	body := io.LimitReader(resp.Body, remoteImageMaxBytes+1)
	limited := &limitedRemoteImageReader{Reader: body, max: remoteImageMaxBytes}
	return s.resourceService().UploadReader(id, typ, name, limited)
}

// validateRemoteImageURL 校验目标 URL：只允许 http/https，且主机解析后的所有 IP
// 都不是内网、环回、链路本地、保留或云元数据地址。
func validateRemoteImageURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errInvalidRemoteImageURL
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedRemoteIP(ip) {
			return errInvalidRemoteImageURL
		}
		return nil
	}
	// 单标签主机名（如 localhost、metadata）视为内网，直接拒绝。
	if !strings.Contains(host, ".") {
		return errInvalidRemoteImageURL
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return errInvalidRemoteImageURL
	}
	if slices.ContainsFunc(ips, isBlockedRemoteIP) {
		return errInvalidRemoteImageURL
	}
	return nil
}

func isBlockedRemoteIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	for _, prefix := range blockedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteImageName(u *url.URL, fallbackName, contentType string) string {
	name := strings.TrimSpace(fallbackName)
	if name == "" {
		name = filepath.Base(u.EscapedPath())
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
	}
	name = strings.Split(name, "?")[0]
	if name == "." || name == "/" || name == "" {
		name = "remote-image"
	}
	ext := filepath.Ext(name)
	if ext == "" {
		ext = remoteImageExt(contentType)
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return stem + "-" + randomAssetToken(6) + ext
}

func randomAssetToken(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	for i, b := range buf {
		buf[i] = chars[int(b)%len(chars)]
	}
	return string(buf)
}

func remoteImageExt(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ".jpg"
	}
	switch strings.ToLower(mediaType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

type limitedRemoteImageReader struct {
	io.Reader
	max  int64
	read int64
}

func (r *limitedRemoteImageReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	if r.read > r.max {
		return n, errRemoteImageTooLarge
	}
	return n, err
}
