package signals

import (
	"context"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/server/endpoints/process"
)

type SignalHandler struct {
	eps process.Endpoints
}

func NewHandler(eps process.Endpoints) *SignalHandler {
	return &SignalHandler{eps: eps}
}

func (h *SignalHandler) BroadcastSignal(ctx context.Context, req *connect.Request[pbendpoints.BroadcastSignalRequest]) (*connect.Response[pbendpoints.BroadcastSignalResponse], error) {
	vars := make(map[string]any)
	if req.Msg.Variables != nil {
		vars = req.Msg.Variables.AsMap()
	}
	_, err := h.eps.BroadcastSignal(ctx, process.BroadcastSignalRequest{
		ProjectID:  req.Msg.ProjectId,
		SignalName: req.Msg.SignalName,
		Variables:  vars,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pbendpoints.BroadcastSignalResponse{}), nil
}

func (h *SignalHandler) SendMessage(ctx context.Context, req *connect.Request[pbendpoints.SendMessageRequest]) (*connect.Response[pbendpoints.SendMessageResponse], error) {
	vars := make(map[string]any)
	if req.Msg.Variables != nil {
		vars = req.Msg.Variables.AsMap()
	}
	_, err := h.eps.SendMessage(ctx, process.SendMessageRequest{
		ProjectID:      req.Msg.ProjectId,
		MessageName:    req.Msg.MessageName,
		CorrelationKey: req.Msg.CorrelationKey,
		Variables:      vars,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pbendpoints.SendMessageResponse{}), nil
}
