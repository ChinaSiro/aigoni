package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Site       SiteConfig       `yaml:"site"`
	Pagination PaginationConfig `yaml:"pagination"`
	Paths      PathsConfig      `yaml:"paths"`
	// Timezone 由 Site.UTCOffset 解析而来，不参与 yaml 序列化。
	// 作为全站日期显示与提交的统一时区基准，避免 UTC/本地时区混用导致 date 漂移。
	Timezone *time.Location `yaml:"-"`
}

type SiteConfig struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Author       string `yaml:"author"`
	BaseURL      string `yaml:"base_url"`
	Logo         string `yaml:"logo"`
	AuthorAvatar string `yaml:"author_avatar"`
	UTCOffset    string `yaml:"utc_offset"`
}

type PaginationConfig struct {
	PostsPerPage   int `yaml:"posts_per_page"`
	HomePostsCount int `yaml:"home_posts_count"`
}

type PathsConfig struct {
	ContentDir string `yaml:"content_dir"`
	PostsDir   string `yaml:"posts_dir"`
	PagesDir   string `yaml:"pages_dir"`
	NotesDir   string `yaml:"notes_dir"`
	PublicDir  string `yaml:"public_dir"`
	UploadsDir string `yaml:"uploads_dir"`
}

type Env struct {
	AdminPassword string
	AigoniAPIKey  string
	Port          int
	AdminPath     string
	// CookieSecure 为 true 时后台会话 Cookie 会带上 Secure 标记，仅用于 HTTPS 部署。
	CookieSecure bool
	// WebFrontendDir and AdminFrontendDir override embedded SPA builds when set.
	WebFrontendDir         string
	AdminFrontendDir       string
	AdminPerPage           int
	UploadAllowedExts      []string
	UploadAllowedExtsLabel string
	WikiAPIKey             string
	WikiBaseURL            string
	WikiModel              string
	// WikiAskAPIEnabled 控制机器只读 Wiki Ask 接口（/api/admin/v1/wiki/ask:api）是否启用；
	// 默认关闭，必须在 .env 中显式开启。
	WikiAskAPIEnabled bool
	// WikiAskAPIRPM 是机器只读 Wiki Ask 提交接口的每分钟请求上限；默认 60。
	WikiAskAPIRPM int
	// WikiAgentConcurrency 是 Admin Wiki Chat 与机器只读 Wiki Ask 共用的
	// active run 总并发上限（pending/running 都计入）；默认 5。
	WikiAgentConcurrency int
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	tz, err := ParseOffset(cfg.Site.UTCOffset)
	if err != nil {
		return nil, fmt.Errorf("site.utc_offset %q: %w", cfg.Site.UTCOffset, err)
	}
	cfg.Timezone = tz
	return &cfg, nil
}

// ParseOffset 把 RFC3339 风格的固定偏移（如 "+08:00"、"-05:00"、"Z"）解析为 *time.Location。
// 用 FixedZone 而非 LoadLocation：不依赖系统 tzdata，容器环境友好。
func ParseOffset(s string) (*time.Location, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse("Z07:00", s)
	if err != nil {
		return nil, err
	}
	_, off := t.Zone()
	return time.FixedZone(s, off), nil
}

func LoadEnv(path string) (*Env, error) {
	values, err := readDotEnv(path)
	if err != nil {
		return nil, err
	}
	keys := []string{
		"ADMIN_PASSWORD",
		"AIGONI_API_KEY",
		"PORT",
		"ADMIN_PATH",
		"COOKIE_SECURE",
		"WEB_FRONTEND_DIR",
		"ADMIN_FRONTEND_DIR",
		"AIGONI_UPLOAD_ALLOWED_EXTS",
		"AIGONI_ADMIN_PER_PAGE",
		"WIKI_APIKEY",
		"WIKI_BASEURL",
		"WIKI_MODEL",
		"AIGONI_WIKI_ASK_API_ENABLED",
		"AIGONI_WIKI_ASK_API_RPM",
		"AIGONI_WIKI_AGENT_CONCURRENCY",
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	port := 8080
	if values["PORT"] != "" {
		port, err = strconv.Atoi(values["PORT"])
		if err != nil {
			return nil, errors.New("PORT must be a number")
		}
	}
	adminPath := strings.Trim(strings.TrimSpace(values["ADMIN_PATH"]), "/")
	if adminPath == "" {
		adminPath = "admin"
	}
	if strings.Contains(adminPath, "/") {
		return nil, errors.New("ADMIN_PATH must be a single URL path segment")
	}
	adminPerPage := 20
	if values["AIGONI_ADMIN_PER_PAGE"] != "" {
		adminPerPage, err = strconv.Atoi(values["AIGONI_ADMIN_PER_PAGE"])
		if err != nil || adminPerPage < 1 {
			return nil, errors.New("AIGONI_ADMIN_PER_PAGE must be a positive number")
		}
	}
	// 机器只读 Wiki Ask 接口默认关闭，必须显式开启；RPM 上限缺省 60。
	wikiAskAPIEnabled := parseBoolEnv(values["AIGONI_WIKI_ASK_API_ENABLED"])
	wikiAskAPIRPM := 60
	if raw := values["AIGONI_WIKI_ASK_API_RPM"]; raw != "" {
		wikiAskAPIRPM, err = strconv.Atoi(raw)
		if err != nil || wikiAskAPIRPM < 1 {
			return nil, errors.New("AIGONI_WIKI_ASK_API_RPM must be a positive number")
		}
	}
	// Wiki Agent 总并发默认 5，Admin Chat 与机器 Ask 共用；非正数或非数字启动时报错。
	wikiAgentConcurrency := 5
	if raw := values["AIGONI_WIKI_AGENT_CONCURRENCY"]; raw != "" {
		wikiAgentConcurrency, err = strconv.Atoi(raw)
		if err != nil || wikiAgentConcurrency < 1 {
			return nil, errors.New("AIGONI_WIKI_AGENT_CONCURRENCY must be a positive number")
		}
	}
	webFrontendDir := strings.TrimSpace(values["WEB_FRONTEND_DIR"])
	adminFrontendDir := strings.TrimSpace(values["ADMIN_FRONTEND_DIR"])
	env := &Env{
		AdminPassword:          values["ADMIN_PASSWORD"],
		AigoniAPIKey:           values["AIGONI_API_KEY"],
		Port:                   port,
		AdminPath:              "/" + adminPath,
		CookieSecure:           parseBoolEnv(values["COOKIE_SECURE"]),
		WebFrontendDir:         webFrontendDir,
		AdminFrontendDir:       adminFrontendDir,
		AdminPerPage:           adminPerPage,
		UploadAllowedExts:      splitExts(values["AIGONI_UPLOAD_ALLOWED_EXTS"]),
		UploadAllowedExtsLabel: strings.Join(splitExts(values["AIGONI_UPLOAD_ALLOWED_EXTS"]), ", "),
		WikiAPIKey:             strings.TrimSpace(values["WIKI_APIKEY"]),
		WikiBaseURL:            strings.TrimRight(strings.TrimSpace(values["WIKI_BASEURL"]), "/"),
		WikiModel:              strings.TrimSpace(values["WIKI_MODEL"]),
		WikiAskAPIEnabled:      wikiAskAPIEnabled,
		WikiAskAPIRPM:          wikiAskAPIRPM,
		WikiAgentConcurrency:   wikiAgentConcurrency,
	}
	if env.AdminPassword == "" {
		return nil, errors.New("ADMIN_PASSWORD is required")
	}
	if err := validateAdminPassword(env.AdminPassword); err != nil {
		return nil, err
	}
	if env.Port < 1 || env.Port > 65535 {
		return nil, errors.New("PORT must be between 1 and 65535")
	}
	return env, nil
}

// parseBoolEnv 解析 "true"/"1" 为 true，其余（含空）为 false。
func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// validateAdminPassword 拒绝过短或常见弱口令，避免公网后台被轻易撞库。
// 包含常见弱口令的密码在环境加载阶段直接拒绝，让不安全部署快速失败。
func validateAdminPassword(password string) error {
	if len(password) < 8 {
		return errors.New("ADMIN_PASSWORD must be at least 8 characters")
	}
	lower := strings.ToLower(strings.TrimSpace(password))
	weak := []string{
		"admin", "password", "passw0rd", "change-me", "changeme",
		"123456", "12345678", "123456789", "qwerty", "letmein",
		"welcome", "iloveyou", "654321", "abc123",
	}
	for _, value := range weak {
		if strings.Contains(lower, value) {
			return fmt.Errorf("ADMIN_PASSWORD is too weak (contains %q)", value)
		}
	}
	if strings.Trim(lower, string(lower[0])) == "" {
		return errors.New("ADMIN_PASSWORD is too weak (repeated characters)")
	}
	return nil
}

func Address(env *Env) string {
	return ":" + strconv.Itoa(env.Port)
}

func Abs(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func splitExts(value string) []string {
	parts := strings.Split(value, ",")
	var out []string
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.ToLower(strings.Trim(strings.TrimSpace(part), "."))
		if part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func applyDefaults(cfg *Config) {
	if cfg.Site.UTCOffset == "" {
		cfg.Site.UTCOffset = "+00:00"
	}
	if cfg.Pagination.PostsPerPage == 0 {
		cfg.Pagination.PostsPerPage = 10
	}
	if cfg.Pagination.HomePostsCount == 0 {
		cfg.Pagination.HomePostsCount = 3
	}
	if cfg.Paths.ContentDir == "" {
		cfg.Paths.ContentDir = "content"
	}
	if cfg.Paths.PostsDir == "" {
		cfg.Paths.PostsDir = "content/posts"
	}
	if cfg.Paths.PagesDir == "" {
		cfg.Paths.PagesDir = "content/pages"
	}
	if cfg.Paths.NotesDir == "" {
		cfg.Paths.NotesDir = "content/notes"
	}
	if cfg.Paths.PublicDir == "" {
		cfg.Paths.PublicDir = "public"
	}
	if cfg.Paths.UploadsDir == "" {
		cfg.Paths.UploadsDir = "public/uploads"
	}
}

func validate(cfg *Config) error {
	if cfg.Site.Name == "" {
		return errors.New("site.name is required")
	}
	return nil
}

func readDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values, scanner.Err()
}
