package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// UpdateDecision saves an edit of one stored version as the next version of
// its key, and makes it live unless asked to stage it.
//
// It used to rewrite the version in place. A decision is a business policy: an
// instance that evaluated version 3 was decided under what version 3 said, and
// its timeline names version 3 — which after an edit said something else, with
// nothing anywhere recording what it had said before.
//
// So the version saved from is only read. The edit becomes the key's next
// version, under the key and project of the version it edits rather than any
// the request names: the key is how processes find the decision, and an edit
// filed under another one would change nothing they consult. A table that is
// the same as the version it was saved from stores nothing and changes nothing
// — see sameDecisionContent for what "the same" means; making an existing
// version live is PromoteDecisionVersion's to do, not a side effect of a save.
func (s *decisionService) UpdateDecision(ctx context.Context, id uuid.UUID, d entities.DecisionDefinition, promote bool) (entities.SavedDecision, error) {
	stored, err := s.repo.Decision().Get(ctx, id)
	if err != nil {
		return entities.SavedDecision{}, err
	}
	// A table with no output column answers nothing, and a save goes live
	// unless asked not to. A request that omits the "decision" object arrives
	// as exactly that empty table, so it is refused rather than made the policy
	// every process consulting the key is given.
	if len(d.Outputs) == 0 {
		return entities.SavedDecision{}, apierr.Invalidf(
			"a decision table needs at least one output column, and this save has none, so it would answer nothing; " +
				"send the whole table in \"decision\"")
	}
	projectID := uuid.UUID(stored.ProjectID)

	d.ID = uuid.Nil
	d.Key = stored.Key
	d.Project = &entities.Project{ID: projectID}

	live, err := s.liveVersion(ctx, projectID, stored.Key)
	if err != nil {
		return entities.SavedDecision{}, err
	}

	same, err := sameDecisionContent(stored, adapters.DecisionModelAdapter{Decision: d}.ToModel())
	if err != nil {
		return entities.SavedDecision{}, err
	}
	if same {
		return entities.SavedDecision{ID: id, Version: stored.Version, Live: stored.Version == live}, nil
	}
	// Staging keeps the live version in force, so it needs one to keep. With
	// none, the version being saved is the only one an unpinned evaluation
	// could read, and staging it would leave the key answering nothing.
	return s.storeNewVersion(ctx, d, promote || live == 0)
}

// liveVersion is the version of key an unpinned evaluation reads, or zero when
// none is live.
func (s *decisionService) liveVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error) {
	live, err := s.repo.Decision().GetLiveByKey(ctx, projectID, key)
	if errors.Is(err, apierr.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("could not read which version of %s is live: %w", key, err)
	}
	return live.Version, nil
}

// storeNewVersion stores d as the next version of its key, and makes it the
// live one when promote says so.
func (s *decisionService) storeNewVersion(ctx context.Context, d entities.DecisionDefinition, promote bool) (entities.SavedDecision, error) {
	if d.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return entities.SavedDecision{}, fmt.Errorf("could not generate a decision id: %w", err)
		}
		d.ID = id
	}

	// The version series is per project, matching the unique index, so the
	// allocator needs the same project the adapter is about to write.
	var projectID uuid.UUID
	if d.Project != nil {
		projectID = d.Project.ID
	}

	err := allocateVersion(ctx, s.repo.UnitOfWork(), "decision "+d.Key,
		func(ctx context.Context) (int, error) {
			return s.repo.Decision().NextVersion(ctx, projectID, d.Key)
		},
		func(txCtx context.Context, version int) error {
			d.Version = version
			if err := s.repo.Decision().Create(txCtx, adapters.DecisionModelAdapter{Decision: d}.ToModel()); err != nil {
				return err
			}
			if !promote {
				return nil
			}
			// In the same transaction as the insert: a save that stored the
			// version and not the choice to put it into force would report
			// success and leave the previous one live.
			return s.repo.Decision().MakeLive(txCtx, projectID, d.Key, version)
		})
	if err != nil {
		return entities.SavedDecision{}, err
	}
	return entities.SavedDecision{ID: d.ID, Version: d.Version, NewVersion: true, Live: promote}, nil
}
