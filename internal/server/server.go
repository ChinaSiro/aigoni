package server

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"aigoni/internal/agent"
	"aigoni/internal/auth"
	"aigoni/internal/config"
	"aigoni/internal/content"
)

type Server struct {
	cfg             *config.Config
	env             *config.Env
	repo            *content.Repository
	auth            *auth.Manager
	root            string
	webFrontendFS   fs.FS
	adminFrontendFS fs.FS
	wikiMu          sync.Mutex
	settingsMu      sync.Mutex
	wikiWriteMu     sync.Mutex
	wikiAgentMu     sync.Mutex
	wikiAgentRunner agent.Runner
	// wikiAgentAskRunner is the dedicated read-only runner for machine Wiki
	// Ask runs. It is built with agent.Config{ReadOnly:true} and never has the
	// write tool, unlike the write-capable browser-admin wikiAgentRunner.
	wikiAgentAskRunner agent.Runner
	// wikiAskLimiter 限制机器只读 Wiki Ask 提交接口的每分钟请求数，线程安全。
	wikiAskLimiter         *rpmLimiter
	wikiAgentCleanupActive bool
	contentWriteMu         sync.Mutex
	wikiAgentRuns          map[string]*wikiAgentRun
	// remoteImageClient 是远程图片下载专用 HTTP 客户端；生产默认带 SSRF 重定向校验。
	remoteImageClient *http.Client
	// remoteImageValidate 控制远程图片初始 URL 的 SSRF 校验，默认启用；测试可关闭以使用环回测试服务器。
	remoteImageValidate bool
}

func New(root string, cfg *config.Config, env *config.Env) *Server {
	return NewWithFrontendFS(root, cfg, env, nil, nil)
}

func NewWithFrontendFS(root string, cfg *config.Config, env *config.Env, webFrontendFS, adminFrontendFS fs.FS) *Server {
	repo := content.NewRepository(
		config.Abs(root, cfg.Paths.PostsDir),
		config.Abs(root, cfg.Paths.PagesDir),
		config.Abs(root, cfg.Paths.NotesDir),
	)
	s := &Server{
		cfg:                 cfg,
		env:                 env,
		repo:                repo,
		auth:                auth.NewManager(env.AdminPassword, env.CookieSecure),
		root:                root,
		webFrontendFS:       webFrontendFS,
		adminFrontendFS:     adminFrontendFS,
		wikiAgentRuns:       map[string]*wikiAgentRun{},
		remoteImageClient:   remoteImageHTTPClient,
		remoteImageValidate: true,
		wikiAskLimiter:      newRPMLimiter(),
	}
	return s
}

func (s *Server) adminPath() string {
	if s.env.AdminPath == "" {
		return "/admin"
	}
	return s.env.AdminPath
}

func (s *Server) adminURL(suffix string) string {
	if suffix == "" {
		return s.adminPath()
	}
	return s.adminPath() + "/" + strings.TrimPrefix(suffix, "/")
}

func (s *Server) reloadConfig() error {
	cfg, err := config.Load(filepath.Join(s.root, "config.yaml"))
	if err != nil {
		return err
	}
	s.cfg = cfg
	s.repo = content.NewRepository(
		config.Abs(s.root, cfg.Paths.PostsDir),
		config.Abs(s.root, cfg.Paths.PagesDir),
		config.Abs(s.root, cfg.Paths.NotesDir),
	)
	return nil
}
