package server

import "context"

func (s *Server) StartDraftCleanup(ctx context.Context) {
	s.resourceService().StartDraftCleanup(ctx)
}
