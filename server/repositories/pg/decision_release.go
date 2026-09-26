package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/decisionrelease"
	"github.com/gsoultan/storm/runtime"
)

// MakeLive records that, from now on, an evaluation of key that names no
// version reads version.
//
// One more entry on the key's timeline rather than an overwrite, so what was
// live before, and when that changed, stays readable. Upserted on (key,
// project, moment) as the process timeline is: two writes naming the same
// instant are one change of mind, not two cutovers.
func (r *decisionRepository) MakeLive(ctx context.Context, projectID uuid.UUID, key string, version int) error {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := decisionrelease.Create()
	ins.SetProjectID(projectID)
	ins.SetDecisionKey(key)
	ins.SetVersion(int64(version))
	ins.SetActivateAt(time.Now().UTC())
	ins.SetDeletedAtNull()
	ins.OnConflictDecisionKeyProjectIDActivateAt()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not make version %d of %s live: %w", version, key, err)
	}
	return nil
}

// GetRelease reports which version of a key is live: the newest entry on its
// timeline whose moment has come.
func (r *decisionRepository) GetRelease(ctx context.Context, projectID uuid.UUID, key string) (models.DecisionReleaseModel, error) {
	// Not found for a project outside the caller's organization, and any
	// other failure as itself: a save reads "nothing is live" as "make this
	// one live", so a failed read must not be mistaken for that answer.
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return models.DecisionReleaseModel{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.DecisionReleaseModel{}, err
	}
	row, found, err := decisionrelease.New().
		Where(
			decisionrelease.ProjectID.Eq(projectID),
			decisionrelease.DecisionKey.Eq(key),
			decisionrelease.ActivateAt.Lte(time.Now().UTC()),
		).
		Order(decisionrelease.ActivateAt.Desc()).
		One(ctx, ex)
	if err != nil {
		return models.DecisionReleaseModel{}, fmt.Errorf("could not read the release timeline of %s: %w", key, err)
	}
	if !found {
		return models.DecisionReleaseModel{}, fmt.Errorf("%w: decision %q has no live version; make one live from its version history",
			apierr.ErrNotFound, key)
	}
	return decisionReleaseFrom(row), nil
}

// GetLiveByKey returns the version of a key an evaluation that names none
// reads.
//
// Strict where the process lookup is lenient. A process key whose timeline
// names nothing falls back to its highest version, which is what processes did
// before they had a timeline. A decision key does not: migration 26 recorded
// the live version of every decision saved before its timeline existed, and
// every save since records one, so a key with none is a table written around
// the service — and evaluating its newest version would put into force a policy
// nobody chose. It is refused, in words, instead.
func (r *decisionRepository) GetLiveByKey(ctx context.Context, projectID uuid.UUID, key string) (models.DecisionDefinitionModel, error) {
	release, err := r.GetRelease(ctx, projectID, key)
	if err != nil {
		return models.DecisionDefinitionModel{}, err
	}
	live, err := r.GetByKeyAndVersion(ctx, projectID, key, release.Version)
	if errors.Is(err, apierr.ErrNotFound) {
		return models.DecisionDefinitionModel{}, fmt.Errorf(
			"%w: decision %q has no live version: v%d was made live and is not there any more; make another version live from its version history",
			apierr.ErrNotFound, key, release.Version)
	}
	return live, err
}

func decisionReleaseFrom(row decisionrelease.Row) models.DecisionReleaseModel {
	return models.DecisionReleaseModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:   models.UUID(row.ProjectID),
		DecisionKey: row.DecisionKey,
		Version:     int(row.Version),
		ActivateAt:  row.ActivateAt,
	}
}
