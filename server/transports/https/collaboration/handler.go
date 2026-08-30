package collaboration

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/collaboration"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps collaboration.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/collaboration/broadcast", httptransport.NewServer(
		eps.BroadcastCollaboration,
		decodeBroadcastCollaborationRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeBroadcastCollaborationRequest(_ context.Context, r *http.Request) (any, error) {
	var req collaboration.BroadcastCollaborationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
