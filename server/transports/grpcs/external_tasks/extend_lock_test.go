package external_tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/external_task"
)

// The gRPC twin hands the endpoint what the worker sent and answers with the
// new expiry, or with the refusal alone: a reply that carried both would let a
// worker reading only the time believe it still holds a task it has lost.
func TestAnExtensionOverGRPCCarriesTheExpiryOrTheRefusalAlone(t *testing.T) {
	until := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	var asked []external_task.ExtendExternalLockRequest
	server := NewServer(external_task.Endpoints{
		ExtendExternalLock: func(_ context.Context, request any) (any, error) {
			req, ok := request.(external_task.ExtendExternalLockRequest)
			if !ok {
				t.Fatalf("the endpoint was handed a %T", request)
			}
			asked = append(asked, req)
			if req.WorkerID != "billing-1" {
				return external_task.ExtendExternalLockResponse{Err: apierr.Invalidf("not this worker's lock — fetch it again")}, nil
			}
			return external_task.ExtendExternalLockResponse{LockExpiration: until}, nil
		},
	})
	taskID := uuid.New()

	extended, err := server.ExtendExternalTaskLock(t.Context(), &pbendpoints.ExtendExternalTaskLockRequest{
		TaskId: taskID.String(), WorkerId: "billing-1", LockDurationMs: 600_000,
	})
	if err != nil || extended.GetError() != "" || !extended.GetLockExpiration().AsTime().Equal(until) {
		t.Fatalf("the holder's extension: %v, error %q, lock_expiration %v; want %v", err, extended.GetError(), extended.GetLockExpiration(), until)
	}
	want := external_task.ExtendExternalLockRequest{TaskID: taskID, WorkerID: "billing-1", LockDuration: 600_000}
	if len(asked) != 1 || asked[0] != want {
		t.Fatalf("the endpoint was asked %+v; want %+v", asked, want)
	}

	refused, err := server.ExtendExternalTaskLock(t.Context(), &pbendpoints.ExtendExternalTaskLockRequest{
		TaskId: taskID.String(), WorkerId: "billing-2", LockDurationMs: 600_000,
	})
	if err != nil || !strings.Contains(refused.GetError(), "fetch it again") || refused.GetLockExpiration() != nil {
		t.Fatalf("another worker's extension: %v, error %q, lock_expiration %v; want the refusal alone", err, refused.GetError(), refused.GetLockExpiration())
	}

	if _, err := server.ExtendExternalTaskLock(t.Context(), &pbendpoints.ExtendExternalTaskLockRequest{
		TaskId: "not-a-task", WorkerId: "billing-1", LockDurationMs: 600_000,
	}); err == nil || !strings.Contains(err.Error(), "not-a-task") {
		t.Fatalf("an id that is not one: %v; want it refused, naming it", err)
	}
	if len(asked) != 2 {
		t.Fatalf("the endpoint was asked %d times; an id that is not one should not reach it", len(asked))
	}
}
