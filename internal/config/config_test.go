package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadEnvReadsPortFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_API_KEY=test\nPORT=9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.Port != 9090 {
		t.Fatalf("Port = %d, want 9090", env.Port)
	}
	if env.AdminPassword != "secret-pass" {
		t.Fatalf("AdminPassword = %q, want secret-pass", env.AdminPassword)
	}
}

func TestLoadEnvReadsAdminPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nADMIN_PATH=control\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.AdminPath != "/control" {
		t.Fatalf("AdminPath = %q, want /control", env.AdminPath)
	}

	defaultPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(defaultPath, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultEnv, err := LoadEnv(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEnv.AdminPath != "/admin" {
		t.Fatalf("default AdminPath = %q, want /admin", defaultEnv.AdminPath)
	}
}

func TestLoadEnvReadsFrontendDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nWEB_FRONTEND_DIR=web/dist\nADMIN_FRONTEND_DIR=admin/dist\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.WebFrontendDir != "web/dist" {
		t.Fatalf("WebFrontendDir = %q, want web/dist", env.WebFrontendDir)
	}
	if env.AdminFrontendDir != "admin/dist" {
		t.Fatalf("AdminFrontendDir = %q, want admin/dist", env.AdminFrontendDir)
	}

	defaultPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(defaultPath, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultEnv, err := LoadEnv(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEnv.WebFrontendDir != "" {
		t.Fatalf("default WebFrontendDir = %q, want empty", defaultEnv.WebFrontendDir)
	}
	if defaultEnv.AdminFrontendDir != "" {
		t.Fatalf("default AdminFrontendDir = %q, want empty", defaultEnv.AdminFrontendDir)
	}
}

func TestLoadEnvAdminPerPage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_ADMIN_PER_PAGE=15\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.AdminPerPage != 15 {
		t.Fatalf("AdminPerPage = %d, want 15", env.AdminPerPage)
	}

	// 缺省回落到 20。
	path2 := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path2, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env2, err := LoadEnv(path2)
	if err != nil {
		t.Fatal(err)
	}
	if env2.AdminPerPage != 20 {
		t.Fatalf("default AdminPerPage = %d, want 20", env2.AdminPerPage)
	}

	// 非法值应报错。
	path3 := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path3, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_ADMIN_PER_PAGE=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnv(path3); err == nil {
		t.Fatal("expected error for non-positive AIGONI_ADMIN_PER_PAGE")
	}
}

func TestLoadEnvReadsWikiAskSwitchAndRPM(t *testing.T) {
	// 缺省：开关关闭，RPM 回落到 60。
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultEnv, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEnv.WikiAskAPIEnabled {
		t.Fatal("default WikiAskAPIEnabled should be false")
	}
	if defaultEnv.WikiAskAPIRPM != 60 {
		t.Fatalf("default WikiAskAPIRPM = %d, want 60", defaultEnv.WikiAskAPIRPM)
	}

	// 显式开启并覆盖 RPM。
	dir2 := t.TempDir()
	path2 := filepath.Join(dir2, ".env")
	if err := os.WriteFile(path2, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_WIKI_ASK_API_ENABLED=true\nAIGONI_WIKI_ASK_API_RPM=5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := LoadEnv(path2)
	if err != nil {
		t.Fatal(err)
	}
	if !env.WikiAskAPIEnabled {
		t.Fatal("WikiAskAPIEnabled should be true")
	}
	if env.WikiAskAPIRPM != 5 {
		t.Fatalf("WikiAskAPIRPM = %d, want 5", env.WikiAskAPIRPM)
	}

	// 非法 RPM（非正数或非数字）应报错。
	for _, bad := range []string{"0", "-1", "abc"} {
		dir3 := t.TempDir()
		path3 := filepath.Join(dir3, ".env")
		if err := os.WriteFile(path3, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_WIKI_ASK_API_RPM="+bad+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadEnv(path3); err == nil {
			t.Fatalf("expected error for invalid AIGONI_WIKI_ASK_API_RPM=%q", bad)
		}
	}
}

func TestLoadEnvReadsWikiAgentConcurrency(t *testing.T) {
	// 缺省回落到 5。
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultEnv, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEnv.WikiAgentConcurrency != 5 {
		t.Fatalf("default WikiAgentConcurrency = %d, want 5", defaultEnv.WikiAgentConcurrency)
	}

	// 显式覆盖。
	dir2 := t.TempDir()
	path2 := filepath.Join(dir2, ".env")
	if err := os.WriteFile(path2, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_WIKI_AGENT_CONCURRENCY=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := LoadEnv(path2)
	if err != nil {
		t.Fatal(err)
	}
	if env.WikiAgentConcurrency != 3 {
		t.Fatalf("WikiAgentConcurrency = %d, want 3", env.WikiAgentConcurrency)
	}

	// 非法值（非正数或非数字）应报错。
	for _, bad := range []string{"0", "-1", "abc"} {
		dir3 := t.TempDir()
		path3 := filepath.Join(dir3, ".env")
		if err := os.WriteFile(path3, []byte("ADMIN_PASSWORD=secret-pass\nAIGONI_WIKI_AGENT_CONCURRENCY="+bad+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadEnv(path3); err == nil {
			t.Fatalf("expected error for invalid AIGONI_WIKI_AGENT_CONCURRENCY=%q", bad)
		}
	}
}

func TestLoadEnvReadsWikiConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nWIKI_APIKEY=key\nWIKI_BASEURL=https://api.example.com/\nWIKI_MODEL=model\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if env.WikiAPIKey != "key" {
		t.Fatalf("WikiAPIKey = %q, want key", env.WikiAPIKey)
	}
	if env.WikiBaseURL != "https://api.example.com" {
		t.Fatalf("WikiBaseURL = %q, want https://api.example.com", env.WikiBaseURL)
	}
	if env.WikiModel != "model" {
		t.Fatalf("WikiModel = %q, want model", env.WikiModel)
	}
}

func TestLoadEnvRejectsWeakAdminPassword(t *testing.T) {
	weakPasswords := []string{
		"secret",         // 过短
		"admin-pass",     // 含弱口令 admin
		"change-me-pass", // 含弱口令 change-me
		"password-123",   // 含弱口令 password
		"aaaaaaaa",       // 重复字符
	}
	for _, password := range weakPasswords {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env")
		data := "ADMIN_PASSWORD=" + password + "\n"
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadEnv(path); err == nil {
			t.Errorf("LoadEnv accepted weak password %q", password)
		}
	}
}

func TestLoadEnvReadsCookieSecure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ADMIN_PASSWORD=secret-pass\nCOOKIE_SECURE=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if !env.CookieSecure {
		t.Fatal("CookieSecure should be true")
	}

	// 缺省为 false，本地 HTTP 开发不受影响。
	defaultPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(defaultPath, []byte("ADMIN_PASSWORD=secret-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultEnv, err := LoadEnv(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEnv.CookieSecure {
		t.Fatal("default CookieSecure should be false")
	}
}

func TestLoadConfigDoesNotRequireServerPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`site:
  name: "Aigoni"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Name != "Aigoni" {
		t.Fatalf("Site.Name = %q, want Aigoni", cfg.Site.Name)
	}
	if cfg.Paths.PostsDir != "content/posts" {
		t.Fatalf("PostsDir = %q, want content/posts", cfg.Paths.PostsDir)
	}
}

func TestParseOffset(t *testing.T) {
	cases := []struct {
		in   string
		want int // 秒
	}{
		{"+08:00", 8 * 3600},
		{"-05:00", -5 * 3600},
		{"+00:00", 0},
		{"Z", 0},
	}
	for _, c := range cases {
		loc, err := ParseOffset(c.in)
		if err != nil {
			t.Fatalf("ParseOffset(%q) err: %v", c.in, err)
		}
		if _, off := time.Now().In(loc).Zone(); off != c.want {
			t.Errorf("ParseOffset(%q) off=%d want %d", c.in, off, c.want)
		}
		// 往返自洽：UTC 05:26 经 +08 应显示为 13:26
		base := time.Date(2026, 6, 15, 5, 26, 0, 0, time.UTC)
		got := base.In(loc).Format("2006-01-02T15:04")
		t.Logf("ParseOffset(%q) → 05:26Z 显示为 %s", c.in, got)
	}
	if _, err := ParseOffset("not-a-zone"); err == nil {
		t.Fatal("ParseOffset(bad) want error")
	}
}

func TestLoadParsesUTCOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`site:
  name: "Aigoni"
  utc_offset: "+08:00"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.UTCOffset != "+08:00" {
		t.Fatalf("UTCOffset = %q", cfg.Site.UTCOffset)
	}
	if cfg.Timezone == nil {
		t.Fatal("Timezone nil")
	}
	if _, off := time.Now().In(cfg.Timezone).Zone(); off != 8*3600 {
		t.Fatalf("Timezone off = %d want 28800", off)
	}
}

func TestLoadDefaultsUTCOffsetToUTC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("site:\n  name: \"Aigoni\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.UTCOffset != "+00:00" {
		t.Fatalf("default UTCOffset = %q want +00:00", cfg.Site.UTCOffset)
	}
}
