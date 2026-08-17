package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"aigoni"
	"aigoni/internal/config"
	"aigoni/internal/server"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := aigoni.EnsureRuntimeFiles(root); err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, "config.yaml"))
	if err != nil {
		log.Fatal(err)
	}
	if err := aigoni.EnsureRuntimeDirs(root,
		cfg.Paths.ContentDir,
		cfg.Paths.PostsDir,
		cfg.Paths.PagesDir,
		cfg.Paths.NotesDir,
		cfg.Paths.PublicDir,
		cfg.Paths.UploadsDir,
		"wiki",
		"wiki/entities",
		"wiki/concepts",
		"wiki/sources",
		"wiki/syntheses",
	); err != nil {
		log.Fatal(err)
	}
	env, err := config.LoadEnv(filepath.Join(root, ".env"))
	if err != nil {
		log.Fatal(err)
	}
	webFrontendFS, err := aigoni.FrontendFS("web")
	if err != nil {
		log.Fatal(err)
	}
	adminFrontendFS, err := aigoni.FrontendFS("admin")
	if err != nil {
		log.Fatal(err)
	}
	srv := server.NewWithFrontendFS(root, cfg, env, webFrontendFS, adminFrontendFS)
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	defer cancelCleanup()
	srv.StartWikiAgentCleanup()
	srv.StartDraftCleanup(cleanupCtx)

	// 显式 http.Server 设置基础超时，避免慢连接长期占住连接。
	// WriteTimeout 保持 0：Wiki Chat 的 SSE 长连接运行上限为 10 分钟，
	// 设置写超时会把流式响应截断。
	httpServer := &http.Server{
		Addr:              config.Address(env),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Aigoni listening on %s", config.Address(env))
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
