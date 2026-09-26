package impl

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A decision key's history, and the choice of which version in it is live.
//
// The same shape as a process's: saving adds a version, the release timeline
// says which one is in force, and any stored version can be put back into
// force. What decisions do not have is scheduling — a cutover arranged for
// later — because nothing needs it yet and it would need a way to see and
// cancel one. The timeline could hold it; the service does not offer it.

// ListDecisionVersions returns every stored version of one key, newest first,
// marking the live one.
//
// Which version is live is the release timeline's answer, read here rather than
// inferred from the list, so the history cannot disagree with what an
// evaluation reads.
func (s *decisionService) ListDecisionVersions(ctx context.Context, projectID uuid.UUID, key string) ([]entities.DecisionVersionStatus, error) {
	if strings.TrimSpace(key) == "" {
		return nil, apierr.Invalidf("a decision key is required")
	}
	versions, err := s.repo.Decision().ListVersionsByKey(ctx, projectID, key)
	if err != nil || len(versions) == 0 {
		return nil, err
	}
	live, err := s.liveVersion(ctx, projectID, key)
	if err != nil {
		return nil, err
	}
	out := make([]entities.DecisionVersionStatus, 0, len(versions))
	for _, v := range versions {
		out = append(out, entities.DecisionVersionStatus{
			ID:        uuid.UUID(v.ID),
			Key:       v.Key,
			Name:      v.Name,
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			Live:      v.Version == live,
		})
	}
	return out, nil
}

// PromoteDecisionVersion makes one stored version the live one.
//
// Nothing is copied: making v2 live after v3 is one more entry on the timeline,
// and v3 stays where it was, to be made live again if the rollback was the
// mistake. The version is checked to exist first — a timeline entry naming a
// version nobody saved would leave every step with no version binding with
// nothing to evaluate.
//
// The version's row is locked for the length of the write, as a deletion locks
// it for its check: a version being made live cannot be deleted in the same
// moment, which would leave the key live on a version that is gone.
func (s *decisionService) PromoteDecisionVersion(ctx context.Context, projectID uuid.UUID, key string, version int) error {
	if strings.TrimSpace(key) == "" {
		return apierr.Invalidf("a decision key is required")
	}
	if version <= 0 {
		return apierr.Invalidf("version must be a positive number, got %d", version)
	}
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		if _, err := s.repo.Decision().LockVersion(txCtx, projectID, key, version); err != nil {
			return err
		}
		live, err := s.liveVersion(txCtx, projectID, key)
		if err != nil || live == version {
			// Already live: another timeline entry would say nothing new.
			return err
		}
		return s.repo.Decision().MakeLive(txCtx, projectID, key, version)
	})
}
