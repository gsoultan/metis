package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processdefinitionrelease"
	"github.com/gsoultan/storm/runtime"
)

type definitionRepository struct{ conn }

// NewDefinitionRepository returns the store of process models and the timeline
// that says which version of each is live.
func NewDefinitionRepository(c *db.Conn) contracts.DefinitionRepository {
	return &definitionRepository{conn{conn: c}}
}

// definitionGraphBatchSize bounds how many definition graphs are held at once.
//
// A definition carries its whole node and flow graph, so scanning every one of
// them into memory is bounded by the largest installation rather than by
// anything this process chose.
const definitionGraphBatchSize = 100

func (r *definitionRepository) Get(ctx context.Context, id uuid.UUID) (models.ProcessDefinitionModel, error) {
	return r.first(ctx, nil, []processdefinition.Pred{processdefinition.ID.Eq(id)})
}

// GetByKey returns the version new instances start on.
//
// "Live" is whichever the release timeline names, and the highest version only
// when it names none. Deploying used to make a version live by being the
// highest number, which is the behaviour the timeline replaced — see
// process_definition_releases.
func (r *definitionRepository) GetByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error) {
	latest, err := r.GetLatestByKey(ctx, key)
	if err != nil {
		return models.ProcessDefinitionModel{}, err
	}
	release, err := r.GetRelease(ctx, uuid.UUID(latest.ProjectID), key)
	if err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return latest, nil
		}
		return models.ProcessDefinitionModel{}, err
	}
	if release.Version == latest.Version {
		return latest, nil
	}
	live, err := r.GetByProjectKeyAndVersion(ctx, uuid.UUID(latest.ProjectID), key, release.Version)
	if err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			// The timeline names a version that is no longer there. Falling
			// back to the highest keeps new instances starting rather than
			// failing every start on a row somebody deleted.
			return latest, nil
		}
		return models.ProcessDefinitionModel{}, err
	}
	return live, nil
}

// GetLatestByKey returns the highest version, whichever is live.
func (r *definitionRepository) GetLatestByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error) {
	return r.first(ctx,
		[]processdefinition.Sort{processdefinition.Version.Desc()},
		[]processdefinition.Pred{processdefinition.Key.Eq(key)})
}

func (r *definitionRepository) GetByKeyAndVersion(ctx context.Context, key string, version int) (models.ProcessDefinitionModel, error) {
	return r.first(ctx, nil, []processdefinition.Pred{
		processdefinition.Key.Eq(key),
		processdefinition.Version.Eq(int64(version)),
	})
}

// GetLiveByProjectKey is the lookup the engine uses.
//
// A process key is unique per project, not per organization, so resolving by
// key alone is ambiguous the moment two projects in one tenant share one — and
// the ambiguity decides which model runs.
func (r *definitionRepository) GetLiveByProjectKey(ctx context.Context, projectID uuid.UUID, key string) (models.ProcessDefinitionModel, error) {
	release, err := r.GetRelease(ctx, projectID, key)
	if err == nil {
		live, err := r.GetByProjectKeyAndVersion(ctx, projectID, key, release.Version)
		if err == nil {
			return live, nil
		}
		if !errors.Is(err, apierr.ErrNotFound) {
			return models.ProcessDefinitionModel{}, err
		}
	} else if !errors.Is(err, apierr.ErrNotFound) {
		return models.ProcessDefinitionModel{}, err
	}
	return r.first(ctx,
		[]processdefinition.Sort{processdefinition.Version.Desc()},
		[]processdefinition.Pred{
			processdefinition.ProjectID.Eq(projectID),
			processdefinition.Key.Eq(key),
		})
}

func (r *definitionRepository) GetByProjectKeyAndVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.ProcessDefinitionModel, error) {
	return r.first(ctx, nil, []processdefinition.Pred{
		processdefinition.ProjectID.Eq(projectID),
		processdefinition.Key.Eq(key),
		processdefinition.Version.Eq(int64(version)),
	})
}

// GetRelease reports which version of a key is live for one project.
//
// The newest row whose moment has passed. That is what makes a cutover arranged
// for 2am happen because 2am arrives rather than because something woke up:
// there is no scheduler, only a comparison.
func (r *definitionRepository) GetRelease(ctx context.Context, projectID uuid.UUID, key string) (models.ProcessDefinitionReleaseModel, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return models.ProcessDefinitionReleaseModel{}, fmt.Errorf("%w: no release for %s", apierr.ErrNotFound, key)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ProcessDefinitionReleaseModel{}, err
	}
	row, found, err := processdefinitionrelease.New().
		Where(
			processdefinitionrelease.ProjectID.Eq(projectID),
			processdefinitionrelease.ProcessKey.Eq(key),
			processdefinitionrelease.ActivateAt.Lte(time.Now().UTC()),
		).
		Order(processdefinitionrelease.ActivateAt.Desc()).
		One(ctx, ex)
	if err != nil {
		return models.ProcessDefinitionReleaseModel{}, fmt.Errorf("could not read the release timeline: %w", err)
	}
	if !found {
		return models.ProcessDefinitionReleaseModel{}, fmt.Errorf("%w: no release for %s", apierr.ErrNotFound, key)
	}
	return releaseFrom(row), nil
}

// ScheduleRelease says that from a moment, a key runs a version.
//
// Upserted on (key, project, moment): arranging the same instant twice is
// somebody changing their mind about which version, not a second cutover.
func (r *definitionRepository) ScheduleRelease(ctx context.Context, projectID uuid.UUID, key string, version int, activateAt time.Time) error {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := processdefinitionrelease.Create()
	ins.SetProjectID(projectID)
	ins.SetProcessKey(key)
	ins.SetVersion(int64(version))
	ins.SetActivateAt(activateAt.UTC())
	ins.SetDeletedAtNull()
	ins.OnConflictProcessKeyProjectIDActivateAt()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not schedule the live version of %s: %w", key, err)
	}
	return nil
}

// ListReleasesForKey returns one key's whole timeline, newest first.
func (r *definitionRepository) ListReleasesForKey(ctx context.Context, projectID uuid.UUID, key string) ([]models.ProcessDefinitionReleaseModel, error) {
	return r.releases(ctx, projectID, []processdefinitionrelease.Pred{processdefinitionrelease.ProcessKey.Eq(key)})
}

// ListReleases returns every key's timeline for one project.
func (r *definitionRepository) ListReleases(ctx context.Context, projectID uuid.UUID) ([]models.ProcessDefinitionReleaseModel, error) {
	return r.releases(ctx, projectID, nil)
}

// DeleteScheduledRelease drops a cutover that has not happened yet.
//
// The `activate_at > now` condition is part of the delete rather than a check
// before it: cancelling a cutover that took effect while somebody was looking
// at the page would otherwise silently un-promote the version that is running.
func (r *definitionRepository) DeleteScheduledRelease(ctx context.Context, projectID uuid.UUID, id uuid.UUID) error {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	removed, err := ex.Exec(ctx,
		`DELETE FROM process_definition_releases
		  WHERE id = $1 AND project_id = $2 AND activate_at > $3`,
		[]any{id, projectID, time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("could not cancel the scheduled release: %w", err)
	}
	if removed == 0 {
		return fmt.Errorf("%w: no such scheduled release (it may have already taken effect)", apierr.ErrNotFound)
	}
	return nil
}

// ListVersionsByKey returns every version deployed under one key, newest first.
func (r *definitionRepository) ListVersionsByKey(ctx context.Context, projectID uuid.UUID, key string) ([]models.ProcessDefinitionModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := processdefinition.New().
		Where(processdefinition.Key.Eq(key)).
		Order(processdefinition.Version.Desc())
	if scoped != nil {
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the versions of %s: %w", key, err)
	}
	return definitionsFrom(rows)
}

func (r *definitionRepository) List(ctx context.Context) ([]models.ProcessDefinitionModel, error) {
	return r.list(ctx, nil)
}

func (r *definitionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessDefinitionModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	return r.list(ctx, scoped)
}

func (r *definitionRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.ProcessDefinitionModel], error) {
	empty := contracts.NewPage([]models.ProcessDefinitionModel{}, 0, p)
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	q := processdefinition.New()
	if scoped != nil {
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scoped)...))
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count definitions: %w", err)
	}
	n := p.Normalize()
	rows, err := q.
		Order(processdefinition.CreatedAt.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list definitions: %w", err)
	}
	items, err := definitionsFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// ScanWithGraphs walks every definition a batch at a time, graphs included.
//
// In batches because a definition carries its whole node and flow graph: the
// backfills that use this would otherwise hold every process model in the
// installation in memory at once, which is bounded by how much somebody
// deployed rather than by anything this process chose.
func (r *definitionRepository) ScanWithGraphs(ctx context.Context, visit func([]models.ProcessDefinitionModel) error) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return nil
	}
	q := processdefinition.New().Order(processdefinition.ID.Asc())
	if !scope.unrestricted() {
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	return r.scanGraphs(ctx, q, visit)
}

// ScanProjectWithGraphs walks one project's definitions — every version of
// every process — a batch at a time, newest first, graphs included.
//
// For what only a graph can say about a whole project, such as which versions
// consult a decision. A list of them stopped at the store's thousand rows, so
// the version a running instance is on could be missed; and holding every
// version at once is bounded by how much the project deployed rather than by
// anything this process chose.
func (r *definitionRepository) ScanProjectWithGraphs(ctx context.Context, projectID uuid.UUID, visit func([]models.ProcessDefinitionModel) error) error {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return err
	}
	// The id breaks ties in creation time, so the cursor is a position.
	q := processdefinition.New().Order(processdefinition.CreatedAt.Desc(), processdefinition.ID.Desc())
	if scoped != nil {
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scoped)...))
	}
	return r.scanGraphs(ctx, q, visit)
}

// scanGraphs hands q's definitions to visit a batch at a time.
//
// Keyset paging rather than OFFSET: a scan that offsets re-reads and discards
// everything before its window, so the last batch of a large installation
// costs as much as the whole table.
func (r *definitionRepository) scanGraphs(ctx context.Context, q processdefinition.Query, visit func([]models.ProcessDefinitionModel) error) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	err = everyBatch(ctx, ex, q, definitionGraphBatchSize, func(rows []processdefinition.Row) error {
		batch, err := definitionsFrom(rows)
		if err != nil {
			return err
		}
		return visit(batch)
	})
	if err != nil {
		return fmt.Errorf("could not scan definitions with graphs: %w", err)
	}
	return nil
}

// ListKeysByProject returns the key of every process a project has, once each,
// in key order.
//
// A project keeps every version it has deployed, so finding its processes by
// reading its definitions read every version, graphs included — and stopped at
// the store's thousand rows, so one process with a thousand newer versions
// pushed every other one out of the answer. One DISTINCT instead; raw SQL,
// because the generated store has none.
func (r *definitionRepository) ListKeysByProject(ctx context.Context, projectID uuid.UUID) ([]string, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT DISTINCT key FROM process_definitions WHERE deleted_at IS NULL`
	var args []any
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		query += " AND project_id = ANY($1)"
	}
	rows, err := ex.Query(ctx, query+" ORDER BY key", args)
	if err != nil {
		return nil, fmt.Errorf("could not list the project's processes: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		keys = append(keys, string(rows.RawValues()[0]))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not list the project's processes: %w", err)
	}
	return keys, nil
}

// NextVersion is the number the next deploy of this key will claim.
//
// Counted across the deleted rows, because the unique key covers them: a
// version number once used is never reissued, and handing out one the
// constraint would refuse is how a deploy fails on its second attempt.
func (r *definitionRepository) NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := ex.Query(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM process_definitions WHERE project_id = $1 AND key = $2`,
		[]any{projectID, key})
	if err != nil {
		return 0, fmt.Errorf("could not read the highest definition version: %w", err)
	}
	defer rows.Close()
	var highest int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &highest); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not read the highest definition version: %w", err)
	}
	return int(highest) + 1, nil
}

func (r *definitionRepository) Create(ctx context.Context, m models.ProcessDefinitionModel) error {
	projectID := uuid.UUID(m.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	nodes, err := json.Marshal(m.Nodes)
	if err != nil {
		return fmt.Errorf("could not encode the definition's nodes: %w", err)
	}
	flows, err := json.Marshal(m.Flows)
	if err != nil {
		return fmt.Errorf("could not encode the definition's flows: %w", err)
	}
	ins := processdefinition.Create()
	if id := uuid.UUID(m.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetKey(m.Key)
	ins.SetName(m.Name)
	ins.SetVersion(int64(m.Version))
	ins.SetNodes(nodes)
	ins.SetFlows(flows)
	if deploymentID := uuid.UUID(m.DeploymentID); deploymentID != uuid.Nil {
		ins.SetDeploymentID(deploymentID)
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			// The violation stays in the chain: the version allocator retries
			// on it, and wrapping it out of sight turns a retry into a failed
			// deploy.
			return fmt.Errorf("%w: version %d of %q already exists (%w)",
				apierr.ErrInvalidArgument, m.Version, m.Key, err)
		}
		return fmt.Errorf("could not create the definition: %w", err)
	}
	return nil
}

func (r *definitionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := processdefinition.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such definition", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the definition: %w", err)
	}
	return nil
}

func (r *definitionRepository) first(ctx context.Context, order []processdefinition.Sort, preds []processdefinition.Pred) (models.ProcessDefinitionModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return models.ProcessDefinitionModel{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ProcessDefinitionModel{}, err
	}
	q := processdefinition.New().Where(preds...)
	if len(order) > 0 {
		q = q.Order(order...)
	}
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return models.ProcessDefinitionModel{}, fmt.Errorf("%w: no such definition", apierr.ErrNotFound)
		}
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return models.ProcessDefinitionModel{}, fmt.Errorf("could not read the definition: %w", err)
	}
	if !found {
		return models.ProcessDefinitionModel{}, fmt.Errorf("%w: no such definition", apierr.ErrNotFound)
	}
	return definitionFrom(row)
}

func (r *definitionRepository) list(ctx context.Context, scoped []uuid.UUID) ([]models.ProcessDefinitionModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// The id breaks ties in creation time, so the cursor is a position.
	q := processdefinition.New().Order(processdefinition.CreatedAt.Desc(), processdefinition.ID.Desc())
	switch {
	case scoped != nil:
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scoped)...))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return nil, nil
		}
		q = q.Where(processdefinition.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	// Every version, as the contract says, not the store's newest thousand.
	rows, err := everyRow[processdefinition.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not list definitions: %w", err)
	}
	return definitionsFrom(rows)
}

func (r *definitionRepository) releases(ctx context.Context, projectID uuid.UUID, preds []processdefinitionrelease.Pred) ([]models.ProcessDefinitionReleaseModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := processdefinitionrelease.New().
		Where(preds...).
		Order(processdefinitionrelease.ActivateAt.Desc())
	if scoped != nil {
		q = q.Where(processdefinitionrelease.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the release timeline: %w", err)
	}
	out := make([]models.ProcessDefinitionReleaseModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, releaseFrom(row))
	}
	return out, nil
}

func releaseFrom(row processdefinitionrelease.Row) models.ProcessDefinitionReleaseModel {
	return models.ProcessDefinitionReleaseModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:  models.UUID(row.ProjectID),
		ProcessKey: row.ProcessKey,
		Version:    int(row.Version),
		ActivateAt: row.ActivateAt,
	}
}

func definitionsFrom(rows []processdefinition.Row) ([]models.ProcessDefinitionModel, error) {
	out := make([]models.ProcessDefinitionModel, 0, len(rows))
	for _, row := range rows {
		d, err := definitionFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func definitionFrom(row processdefinition.Row) (models.ProcessDefinitionModel, error) {
	d := models.ProcessDefinitionModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID: models.UUID(row.ProjectID),
		Key:       row.Key,
		Name:      row.Name,
		Version:   int(row.Version),
	}
	if deploymentID, ok := row.DeploymentID.Get(); ok {
		d.DeploymentID = models.UUID(deploymentID)
	}
	if len(row.Nodes) > 0 {
		if err := json.Unmarshal(row.Nodes, &d.Nodes); err != nil {
			return models.ProcessDefinitionModel{}, fmt.Errorf("could not decode a definition's nodes: %w", err)
		}
	}
	if len(row.Flows) > 0 {
		if err := json.Unmarshal(row.Flows, &d.Flows); err != nil {
			return models.ProcessDefinitionModel{}, fmt.Errorf("could not decode a definition's flows: %w", err)
		}
	}
	return d, nil
}
