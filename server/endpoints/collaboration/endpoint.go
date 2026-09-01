package collaboration

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	BroadcastCollaboration endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		BroadcastCollaboration: MakeBroadcastCollaborationEndpoint(s),
	}
}

func MakeBroadcastCollaborationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(BroadcastCollaborationRequest)
		if !ok {
			return nil, fmt.Errorf("collaboration: expected a BroadcastCollaborationRequest, got %T", request)
		}
		err := s.Broadcast(ctx, req.Event)
		return BroadcastCollaborationResponse{Err: err}, nil
	}
}
