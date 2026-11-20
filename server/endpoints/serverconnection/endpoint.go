package serverconnection

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Endpoints connect this server to the ones releases come from and go to.
type Endpoints struct {
	ListServerConnections    endpoint.Endpoint
	MakeServerConnectionCode endpoint.Endpoint
	CreateServerConnection   endpoint.Endpoint
	AcceptServerConnection   endpoint.Endpoint
	RemoveServerConnection   endpoint.Endpoint
	// AnswerServerHandshake is public: the code admits it.
	AnswerServerHandshake endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListServerConnections: func(ctx context.Context, _ any) (any, error) {
			server, err := s.ServerConnectionIdentity(ctx)
			if err != nil {
				return ListResponse{Err: err}, nil
			}
			connections, err := s.ListServerConnections(ctx)
			if err != nil {
				return ListResponse{Err: err}, nil
			}
			return ListResponse{Server: &server, Connections: connections}, nil
		},
		MakeServerConnectionCode: func(ctx context.Context, _ any) (any, error) {
			code, err := s.MakeServerConnectionCode(ctx)
			if err != nil {
				return CodeResponse{Err: err}, nil
			}
			return CodeResponse{Code: &code}, nil
		},
		CreateServerConnection: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(ConnectRequest)
			if !ok {
				return nil, fmt.Errorf("server connection: expected a ConnectRequest, got %T", request)
			}
			c, err := s.CreateServerConnection(ctx, req.URL, req.Code)
			if err != nil {
				return ConnectionResponse{Err: err}, nil
			}
			return ConnectionResponse{Connection: &c}, nil
		},
		AcceptServerConnection: func(ctx context.Context, request any) (any, error) {
			id, err := connectionID(request)
			if err != nil {
				return ConnectionResponse{Err: err}, nil
			}
			c, err := s.AcceptServerConnection(ctx, id)
			if err != nil {
				return ConnectionResponse{Err: err}, nil
			}
			return ConnectionResponse{Connection: &c}, nil
		},
		RemoveServerConnection: func(ctx context.Context, request any) (any, error) {
			id, err := connectionID(request)
			if err != nil {
				return RemoveResponse{Err: err}, nil
			}
			return RemoveResponse{Err: s.RemoveServerConnection(ctx, id)}, nil
		},
		AnswerServerHandshake: func(ctx context.Context, request any) (any, error) {
			hello, ok := request.(contracts.ServerHandshake)
			if !ok {
				return nil, fmt.Errorf("server connection: expected a ServerHandshake, got %T", request)
			}
			reply, err := s.AnswerServerHandshake(ctx, hello)
			if err != nil {
				return HandshakeResponse{Err: err}, nil
			}
			return HandshakeResponse{ServerHandshakeReply: reply}, nil
		},
	}
}

func connectionID(request any) (uuid.UUID, error) {
	req, ok := request.(IDRequest)
	if !ok {
		return uuid.Nil, fmt.Errorf("server connection: expected an IDRequest, got %T", request)
	}
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not a connection id", req.ID)
	}
	return id, nil
}
