package server

import "net/http"

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)

	mux.HandleFunc("GET /rest/v1", s.restIndex)
	mux.HandleFunc("GET /rest/v1/", s.restIndex)
	mux.HandleFunc("GET /rest/v1/site", s.restSite)
	mux.HandleFunc("GET /rest/v1/categories", s.restCategories)
	mux.HandleFunc("GET /rest/v1/tags", s.restTags)
	mux.HandleFunc("GET /rest/v1/archives", s.restArchives)
	mux.HandleFunc("GET /rest/v1/posts", s.restPosts)
	mux.HandleFunc("GET /rest/v1/posts/", s.restPostDetail)
	mux.HandleFunc("GET /rest/v1/pages", s.restPages)
	mux.HandleFunc("GET /rest/v1/pages/", s.restPageDetail)
	mux.HandleFunc("GET /rest/v1/search", s.restSearch)

	s.registerAdminAPIRoutes(mux)
	mux.HandleFunc("GET /login", s.adminFrontend)
	adminPath := s.adminPath()
	mux.HandleFunc("GET "+adminPath, s.adminFrontend)
	mux.HandleFunc("GET "+adminPath+"/", s.adminFrontend)

	mux.HandleFunc("GET /uploads/", s.upload)
	mux.HandleFunc("GET /assets/", s.asset)
	mux.HandleFunc("GET /_app/web/", s.webFrontend)
	mux.HandleFunc("GET /_app/admin/", s.adminFrontend)

	mux.Handle("POST /api/content", s.requireAPIKey(http.HandlerFunc(s.apiCreateContent)))

	// Machine read-only Wiki Ask: authenticated with AIGONI_API_KEY, bypasses
	// browser Session/CSRF, and runs the read-only agent (no write tool).
	mux.Handle("POST /api/admin/v1/wiki/ask:api", s.requireAPIKey(http.HandlerFunc(s.adminWikiAPIAskMachine)))
	mux.Handle("GET /api/admin/v1/wiki/ask/runs/{id}", s.requireAPIKey(http.HandlerFunc(s.adminWikiAPIAskRunMachine)))

	mux.HandleFunc("GET /", s.webFrontend)

	return mux
}
