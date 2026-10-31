package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/release"
	"github.com/gsoultan/storm/runtime"
)

type releaseRepository struct{ conn }

// NewReleaseRepository returns the store of the releases a runtime holds.
func NewReleaseRepository(c *db.Conn) contracts.ReleaseRepository {
	return &releaseRepository{conn{conn: c}}
}

// Create records a release.
func (r *releaseRepository) Create(ctx context.Context, rel entities.Release) (entities.Release, error) {
	if rel.Project == nil || rel.Name == "" {
		return entities.Release{}, errors.New("a release names a project and itself")
	}
	if err := r.requireProjectInTenant(ctx, rel.Project.ID); err != nil {
		return entities.Release{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.Release{}, err
	}
	if _, found, err := release.New().
		Where(release.ProjectID.Eq(rel.Project.ID), release.Name.Eq(rel.Name)).
		One(ctx, ex); err != nil {
		return entities.Release{}, fmt.Errorf("could not read the project's releases: %w", err)
	} else if found {
		return entities.Release{}, apierr.Invalidf("this project already has a release named %q here; give the new one another name", rel.Name)
	}
	bundle, items, err := encodeRelease(rel)
	if err != nil {
		return entities.Release{}, err
	}
	ins := release.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetProjectID(rel.Project.ID)
	ins.SetName(rel.Name)
	ins.SetHash(rel.Hash)
	ins.SetOrigin(string(rel.Origin))
	ins.SetState(string(rel.State))
	ins.SetBundle(bundle)
	ins.SetItems(items)
	ins.SetCutBy(rel.CutBy)
	ins.SetActedBy(rel.ActedBy)
	ins.SetPrevious(rel.Previous)
	if rel.LiveAt != nil {
		ins.SetLiveAt(*rel.LiveAt)
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.Release{}, fmt.Errorf("could not save the release: %w", err)
	}
	return promotionReleaseFrom(row)
}

// GetByName reads one release of a project.
func (r *releaseRepository) GetByName(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error) {
	return r.one(ctx, projectID, release.Name.Eq(name), "no release named "+name)
}

// GetLive reads the project's live release here.
func (r *releaseRepository) GetLive(ctx context.Context, projectID uuid.UUID) (entities.Release, error) {
	return r.one(ctx, projectID, release.State.Eq(string(entities.ReleaseLive)), "no release is live here")
}

func (r *releaseRepository) one(ctx context.Context, projectID uuid.UUID, pred release.Pred, missing string) (entities.Release, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return entities.Release{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.Release{}, err
	}
	row, found, err := release.New().
		Where(release.ProjectID.Eq(projectID), pred).
		Order(release.CreatedAt.Desc(), release.ID.Desc()).
		One(ctx, ex)
	if err != nil {
		return entities.Release{}, fmt.Errorf("could not read the release: %w", err)
	}
	if !found {
		return entities.Release{}, fmt.Errorf("%w: %s", apierr.ErrNotFound, missing)
	}
	return promotionReleaseFrom(row)
}

// List reads a project's releases, newest first.
func (r *releaseRepository) List(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.Release, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := release.New().
		Where(release.ProjectID.Eq(projectID)).
		Order(release.CreatedAt.Desc(), release.ID.Desc()).
		Limit(int64(limit)).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the project's releases: %w", err)
	}
	out := make([]entities.Release, 0, len(rows))
	for _, row := range rows {
		rel, err := promotionReleaseFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, nil
}

// Update rewrites a release's state, items and who last acted on it. The
// bundle and its hash are what the release is, and are never rewritten.
func (r *releaseRepository) Update(ctx context.Context, rel entities.Release) error {
	if rel.Project == nil {
		return errors.New("a release names its project")
	}
	if err := r.requireProjectInTenant(ctx, rel.Project.ID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := release.New().Where(release.ID.Eq(rel.ID), release.ProjectID.Eq(rel.Project.ID)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the release: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such release", apierr.ErrNotFound)
	}
	_, items, err := encodeRelease(rel)
	if err != nil {
		return err
	}
	mut := release.Mutate(row)
	mut.SetState(string(rel.State))
	mut.SetItems(items)
	mut.SetActedBy(rel.ActedBy)
	mut.SetPrevious(rel.Previous)
	if rel.LiveAt != nil {
		mut.SetLiveAt(*rel.LiveAt)
	}
	mut.SetUpdatedAtNow()
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not save the release: %w", err)
	}
	return nil
}

func encodeRelease(rel entities.Release) (bundle, items []byte, err error) {
	if bundle, err = json.Marshal(rel.Bundle); err != nil {
		return nil, nil, fmt.Errorf("could not encode the release: %w", err)
	}
	if rel.Items == nil {
		rel.Items = []entities.ReleaseItem{}
	}
	if items, err = json.Marshal(rel.Items); err != nil {
		return nil, nil, fmt.Errorf("could not encode the release's versions: %w", err)
	}
	return bundle, items, nil
}

func promotionReleaseFrom(row release.Row) (entities.Release, error) {
	rel := entities.Release{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		Project:   &entities.Project{ID: row.ProjectID},
		Name:      row.Name,
		Hash:      row.Hash,
		Origin:    entities.ReleaseOrigin(row.Origin),
		State:     entities.ReleaseState(row.State),
		CutBy:     row.CutBy,
		ActedBy:   row.ActedBy,
		Previous:  row.Previous,
		LiveAt:    liveAtOf(row.LiveAt),
	}
	if err := json.Unmarshal(row.Bundle, &rel.Bundle); err != nil {
		return entities.Release{}, fmt.Errorf("release %s cannot be read: %w", row.Name, err)
	}
	if err := json.Unmarshal(row.Items, &rel.Items); err != nil {
		return entities.Release{}, fmt.Errorf("release %s has versions that cannot be read: %w", row.Name, err)
	}
	return rel, nil
}

func liveAtOf(v runtime.Null[time.Time]) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.V
	return &t
}
