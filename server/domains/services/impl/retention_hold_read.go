package impl

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// retentionHoldListLimit is the most holds a project's list returns. Holds
// are placed one at a time by an administrator; a project with more than this
// is a project whose holds are kept some other way.
const retentionHoldListLimit = 1000

// InstanceHold reads the hold on an instance the caller may read. An instance
// that is not there, or is not the caller's, is not found rather than not
// held.
func (s *retentionHoldService) InstanceHold(ctx context.Context, instanceID uuid.UUID) (entities.RetentionHold, bool, error) {
	if _, err := s.repo.Process().Get(ctx, instanceID); err != nil {
		return entities.RetentionHold{}, false, err
	}
	return s.repo.RetentionHold().ForInstance(ctx, instanceID)
}

// ListRetentionHolds lists a project's holds, newest first.
func (s *retentionHoldService) ListRetentionHolds(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionHold, error) {
	return s.repo.RetentionHold().ListByProject(ctx, projectID, retentionHoldListLimit)
}
