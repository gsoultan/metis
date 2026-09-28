package adapters

import (
	"time"

	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ExtendLockReplyToProto is the reply to a lock extension over Connect and
// gRPC: when the lock now runs out, or why it was not extended, with no time
// beside the reason. One place for both, so neither can tell a worker whose
// extension was refused that its lock runs until some time.
func ExtendLockReplyToProto(until time.Time, refused error) *pbendpoints.ExtendExternalTaskLockResponse {
	if refused != nil {
		return &pbendpoints.ExtendExternalTaskLockResponse{Error: redaction.RedactError(refused)}
	}
	return &pbendpoints.ExtendExternalTaskLockResponse{LockExpiration: timestamppb.New(until)}
}
