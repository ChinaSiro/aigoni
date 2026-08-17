package server

func (s *Server) wikiReady() bool {
	return s.env.WikiAPIKey != "" && s.env.WikiBaseURL != "" && s.env.WikiModel != ""
}
