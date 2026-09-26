package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// UpdateDecision saves an edit of one stored version as the next version of
// its key.
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
// the same as the version it was saved from stores nothing — see
// sameDecisionContent for what "the same" means.
func (s *decisionService) UpdateDecision(ctx context.Context, id uuid.UUID, d entities.DecisionDefinition) (entities.SavedDecision, error) {
	stored, err := s.repo.Decision().Get(ctx, id)
	if err != nil {
		return entities.SavedDecision{}, err
	}

	d.ID = uuid.Nil
	d.Key = stored.Key
	d.Project = &entities.Project{ID: uuid.UUID(stored.ProjectID)}

	same, err := sameDecisionContent(stored, adapters.DecisionModelAdapter{Decision: d}.ToModel())
	if err != nil {
		return entities.SavedDecision{}, err
	}
	if same {
		return entities.SavedDecision{ID: id, Version: stored.Version}, nil
	}
	return s.storeNewVersion(ctx, d)
}

// storeNewVersion stores d as the next version of its key, and makes it the
// live one.
func (s *decisionService) storeNewVersion(ctx context.Context, d entities.DecisionDefinition) (entities.SavedDecision, error) {
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
			// In the same transaction as the insert: a save that stored the
			// version and not the choice to put it into force would report
			// success and leave the previous one live.
			return s.repo.Decision().MakeLive(txCtx, projectID, d.Key, version)
		})
	if err != nil {
		return entities.SavedDecision{}, err
	}
	return entities.SavedDecision{ID: d.ID, Version: d.Version, NewVersion: true}, nil
}
