package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ApprovalLineService runs approval lines: a list of approvers defined as
// data, which metis deploys as an ordinary process definition.
type ApprovalLineService interface {
	// DefineApprovalLine deploys line as the next version of its key.
	DefineApprovalLine(ctx context.Context, projectID uuid.UUID, line entities.ApprovalLine) (entities.ApprovalLine, error)
	// GetApprovalLine reads the line the key's live version is.
	GetApprovalLine(ctx context.Context, projectID uuid.UUID, key string) (entities.ApprovalLine, error)
	// SubmitApproval starts a request on the line, as actor.
	SubmitApproval(ctx context.Context, projectID uuid.UUID, key string, vars map[string]any, actor string) (entities.ApprovalRequest, error)
	// DecideApproval decides an open task of a line, as actor. Variables are
	// taken only with a resubmit, for the corrections the submitter made.
	DecideApproval(ctx context.Context, taskID uuid.UUID, decision, comment string, vars map[string]any, actor string) (entities.ApprovalRequest, error)
	// ListApprovalRequests lists a line's requests, newest first, at most
	// MaxApprovalRequestsListed, across every version; only submittedBy's when
	// it is not empty. Each carries its steps' decisions but not who a step
	// waits on: GetApprovalRequest says that.
	ListApprovalRequests(ctx context.Context, projectID uuid.UUID, key, submittedBy string) ([]entities.ApprovalRequest, error)
	// GetApprovalRequest says where a request is: each step, who decided, and
	// who it waits on.
	GetApprovalRequest(ctx context.Context, instanceID uuid.UUID) (entities.ApprovalRequest, error)
	// ListApprovalsBehind lists the open requests on versions older than the
	// live one, with what moving each version's requests would do.
	ListApprovalsBehind(ctx context.Context, projectID uuid.UUID, key string) (entities.ApprovalTakeover, error)
	// MoveApprovals moves open requests onto the live version, as actor, by
	// the same migration, and under the same rules, an administrator applies
	// in the designer. A request the migration would refuse is left, with why.
	MoveApprovals(ctx context.Context, projectID uuid.UUID, key string, instanceIDs []uuid.UUID, actor string) (entities.ApprovalMoveResult, error)
}
