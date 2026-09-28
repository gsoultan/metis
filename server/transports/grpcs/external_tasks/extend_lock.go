package external_tasks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/external_task"
	"github.com/gsoultan/metis/server/transports/adapters"
)

// ExtendExternalTaskLock gives the worker holding a task's lock more time. A
// refusal travels in the reply's error field, as on the rest of this service,
// and with no lock_expiration beside it.
func (s *Server) ExtendExternalTaskLock(ctx context.Context, req *pbendpoints.ExtendExternalTaskLockRequest) (*pbendpoints.ExtendExternalTaskLockResponse, error) {
	_, resp, err := s.extendExternalLock.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*pbendpoints.ExtendExternalTaskLockResponse)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected a *pbendpoints.ExtendExternalTaskLockResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCExtendExternalLockRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*pbendpoints.ExtendExternalTaskLockRequest)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected a *pbendpoints.ExtendExternalTaskLockRequest, got %T", grpcReq)
	}
	id, err := uuid.Parse(req.TaskId)
	if err != nil {
		return nil, apierr.Invalidf("the external task id %q is not an id", req.TaskId)
	}
	return external_task.ExtendExternalLockRequest{
		TaskID:       id,
		WorkerID:     req.WorkerId,
		LockDuration: req.LockDurationMs,
	}, nil
}

func encodeGRPCExtendExternalLockResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(external_task.ExtendExternalLockResponse)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected an external_task.ExtendExternalLockResponse, got %T", response)
	}
	return adapters.ExtendLockReplyToProto(resp.LockExpiration, resp.Err), nil
}
