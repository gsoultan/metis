package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/decisiondefinition"
	"github.com/gsoultan/storm/runtime"
)

type decisionRepository struct{ conn }

// NewDecisionRepository returns the store of decision tables.
func NewDecisionRepository(c *db.Conn) contracts.DecisionRepository {
	return &decisionRepository{conn{conn: c}}
}

func (r *decisionRepository) Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error) {
	return r.one(ctx, decisiondefinition.ID.Eq(id))
}

func (r *decisionRepository) GetByKeyAndVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.DecisionDefinitionModel, error) {
	return r.one(ctx,
		decisiondefinition.ProjectID.Eq(projectID),
		decisiondefinition.Key.Eq(key),
		decisiondefinition.Version.Eq(int64(version)),
	)
}

func (r *decisionRepository) List(ctx context.Context) ([]models.DecisionDefinitionModel, error) {
	return r.list(ctx, nil)
}

func (r *decisionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DecisionDefinitionModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	return r.list(ctx, scoped)
}

func (r *decisionRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, search string, p contracts.Pagination) (contracts.Page[models.DecisionDefinitionModel], error) {
	empty := contracts.NewPage([]models.DecisionDefinitionModel{}, 0, p)
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}

	q := decisiondefinition.New()
	if scoped != nil {
		q = q.Where(decisiondefinition.ProjectID.In(uuidsToRaw(scoped)...))
	}
	if pattern, ok := containsPattern(search); ok {
		q = q.Any(decisiondefinition.Name.ILike(pattern), decisiondefinition.Key.ILike(pattern))
	}
	// The count is a separate statement over the same predicate. A page without
	// a total cannot say "51-100 of 1,234", which is the whole reason the caller
	// asked for a page rather than a slice.
	total, err := q.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count decisions: %w", err)
	}
	n := p.Normalize()
	rows, err := q.
		Order(decisiondefinition.CreatedAt.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not list decisions: %w", err)
	}
	items, err := decisionsFrom(rows)
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// NextVersion is the number the next deploy of this key will claim.
//
// Unscoped by deleted rows on purpose: the unique key covers them, so a version
// number once used is never reissued even if its row is removed. Handing out a
// number the constraint would refuse is how a deploy fails on its second
// attempt with a duplicate key.
func (r *decisionRepository) NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	var highest int64
	rows, err := ex.Query(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM decision_definitions WHERE project_id = $1 AND key = $2`,
		[]any{projectID, key})
	if err != nil {
		return 0, fmt.Errorf("could not read the highest decision version: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &highest); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not read the highest decision version: %w", err)
	}
	return int(highest) + 1, nil
}

func (r *decisionRepository) Create(ctx context.Context, d models.DecisionDefinitionModel) error {
	projectID := uuid.UUID(d.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := decisiondefinition.Create()
	if id := uuid.UUID(d.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetKey(d.Key)
	ins.SetName(d.Name)
	ins.SetVersion(int64(d.Version))
	ins.SetHitPolicy(d.HitPolicy)
	setOrNullString(ins.SetAggregation, ins.SetAggregationNull, d.Aggregation)
	if err := encodeDecision(ins.SetRequiredDecisions, ins.SetInputs, ins.SetOutputs, ins.SetRules, ins.SetTests, d); err != nil {
		return err
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			// The unique violation stays in the chain. The version allocator
			// retries on it — two deploys racing for the same number is
			// contention, not a bad request — and wrapping it in something the
			// allocator cannot recognise turns a retry into a failed deploy.
			return fmt.Errorf("%w: version %d of %q already exists (%w)",
				apierr.ErrInvalidArgument, d.Version, d.Key, err)
		}
		return fmt.Errorf("could not create the decision: %w", err)
	}
	return nil
}

func (r *decisionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := decisiondefinition.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such decision", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the decision: %w", err)
	}
	return nil
}

func (r *decisionRepository) one(ctx context.Context, preds ...decisiondefinition.Pred) (models.DecisionDefinitionModel, error) {
	return r.first(ctx, nil, preds)
}

func (r *decisionRepository) first(ctx context.Context, order []decisiondefinition.Sort, preds []decisiondefinition.Pred) (models.DecisionDefinitionModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return models.DecisionDefinitionModel{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.DecisionDefinitionModel{}, err
	}
	q := decisiondefinition.New().Where(preds...)
	if len(order) > 0 {
		q = q.Order(order...)
	}
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return models.DecisionDefinitionModel{}, fmt.Errorf("%w: no such decision", apierr.ErrNotFound)
		}
		q = q.Where(decisiondefinition.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return models.DecisionDefinitionModel{}, fmt.Errorf("could not read the decision: %w", err)
	}
	if !found {
		return models.DecisionDefinitionModel{}, fmt.Errorf("%w: no such decision", apierr.ErrNotFound)
	}
	return decisionFrom(row)
}

func (r *decisionRepository) list(ctx context.Context, scoped []uuid.UUID) ([]models.DecisionDefinitionModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := decisiondefinition.New().Order(decisiondefinition.CreatedAt.Desc())
	switch {
	case scoped != nil:
		q = q.Where(decisiondefinition.ProjectID.In(uuidsToRaw(scoped)...))
	case !scope.unrestricted():
		if len(scope.projects) == 0 {
			return nil, nil
		}
		q = q.Where(decisiondefinition.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list decisions: %w", err)
	}
	return decisionsFrom(rows)
}

func encodeDecision(setRequired, setInputs, setOutputs, setRules, setTests func(runtime.JSON), d models.DecisionDefinitionModel) error {
	for _, part := range []struct {
		name  string
		value any
		set   func(runtime.JSON)
	}{
		{"required decisions", d.RequiredDecisions, setRequired},
		{"inputs", d.Inputs, setInputs},
		{"outputs", d.Outputs, setOutputs},
		{"rules", d.Rules, setRules},
		{"tests", d.Tests, setTests},
	} {
		encoded, err := json.Marshal(part.value)
		if err != nil {
			return fmt.Errorf("could not encode the decision's %s: %w", part.name, err)
		}
		part.set(encoded)
	}
	return nil
}

func decisionsFrom(rows []decisiondefinition.Row) ([]models.DecisionDefinitionModel, error) {
	out := make([]models.DecisionDefinitionModel, 0, len(rows))
	for _, row := range rows {
		d, err := decisionFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func decisionFrom(row decisiondefinition.Row) (models.DecisionDefinitionModel, error) {
	d := models.DecisionDefinitionModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:   models.UUID(row.ProjectID),
		Key:         row.Key,
		Name:        row.Name,
		Version:     int(row.Version),
		HitPolicy:   row.HitPolicy,
		Aggregation: valueOr(row.Aggregation),
	}
	for _, part := range []struct {
		name string
		raw  runtime.JSON
		into any
	}{
		{"required decisions", row.RequiredDecisions, &d.RequiredDecisions},
		{"inputs", row.Inputs, &d.Inputs},
		{"outputs", row.Outputs, &d.Outputs},
		{"rules", row.Rules, &d.Rules},
		{"tests", row.Tests, &d.Tests},
	} {
		if len(part.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(part.raw, part.into); err != nil {
			return models.DecisionDefinitionModel{}, fmt.Errorf("could not decode a decision's %s: %w", part.name, err)
		}
	}
	return d, nil
}
