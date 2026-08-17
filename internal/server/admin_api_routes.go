package server

import (
	"net/http"
	"strings"

	"aigoni/internal/content"
)

func (s *Server) registerAdminAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+adminAPIBasePath+"/session", s.adminSession)
	mux.HandleFunc("GET "+adminAPIBasePath+"/session", s.adminSession)
	mux.Handle("DELETE "+adminAPIBasePath+"/session", s.requireAdminAPIAuth(http.HandlerFunc(s.adminSession)))

	auth := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, s.requireAdminAPIAuth(handler))
	}
	auth("GET "+adminAPIBasePath+"/dashboard", s.adminAPIDashboard)
	auth("GET "+adminAPIBasePath+"/settings", s.settingsAPI)
	auth("PATCH "+adminAPIBasePath+"/settings", s.settingsAPI)
	auth("PUT "+adminAPIBasePath+"/settings/assets/{kind}", s.settingsAssetsAPI)
	auth("DELETE "+adminAPIBasePath+"/settings/assets/{kind}", s.settingsAssetsAPI)
	registerAdminContentRoutes(s, auth)
	s.registerAdminWikiAPIRoutes(mux)
	s.registerAdminAssetsAPIRoutes(mux)
	mux.HandleFunc("GET "+adminAPIBasePath+"/", s.adminAPINotFound)
}

func registerAdminContentRoutes(s *Server, auth func(string, http.HandlerFunc)) {
	for _, route := range []struct {
		name string
		typ  content.Type
	}{
		{name: "posts", typ: content.TypePost},
		{name: "pages", typ: content.TypePage},
		{name: "notes", typ: content.TypeNote},
	} {
		route := route
		base := adminAPIBasePath + "/" + route.name
		auth("GET "+base, func(w http.ResponseWriter, r *http.Request) {
			s.adminAPIContentList(w, r, route.typ)
		})
		auth("POST "+base, s.adminAPIContent)
		auth("GET "+base+"/", func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(strings.TrimPrefix(r.URL.Path, base+"/"), "/assets") {
				s.adminResourcesAPI(w, r)
				return
			}
			s.adminAPIContentDetail(w, r, route.typ, base+"/")
		})
		auth("POST "+base+"/", s.adminResourcesAPI)
		auth("PUT "+base+"/", s.adminResourcesAPI)
		auth("PATCH "+base+"/", s.adminAPIContent)
		auth("DELETE "+base+"/", func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(strings.TrimPrefix(r.URL.Path, base+"/"), "/assets") {
				s.adminResourcesAPI(w, r)
				return
			}
			s.adminAPIContent(w, r)
		})
	}
	auth("GET "+adminAPIBasePath+"/categories", s.adminAPICategories)
	auth("GET "+adminAPIBasePath+"/note-categories", s.adminAPINoteCategories)
	auth("GET "+adminAPIBasePath+"/tags", s.adminAPITags)
	auth("GET "+adminAPIBasePath+"/note-tags", s.adminAPINoteTags)
	auth("GET "+adminAPIBasePath+"/search", s.adminAPISearch)
}
