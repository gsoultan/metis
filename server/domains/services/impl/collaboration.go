package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

type collaborationService struct {
	sse *impl.SSEObserver
}

func NewCollaborationService(sse *impl.SSEObserver) contracts.CollaborationService {
	return &collaborationService{sse: sse}
}

func (s *collaborationService) Broadcast(ctx context.Context, event any) error {
	s.sse.Broadcast(event)
	return nil
}
