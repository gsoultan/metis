package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/simulationcase"
)

type simulationCaseRepository struct{ conn }

// NewSimulationCaseRepository returns the store of the simulation test cases
// processes keep.
func NewSimulationCaseRepository(c *db.Conn) contracts.SimulationCaseRepository {
	return &simulationCaseRepository{conn{conn: c}}
}

// errTooManySimulationCases refuses a case past the bound a process keeps.
var errTooManySimulationCases = apierr.Invalidf(
	"a process keeps at most %d simulation cases; delete one before saving another",
	entities.MaxSimulationCasesPerProcess)

// ListByProcess lists one process's cases, oldest first.
//
// Read up to one past the bound, not to it: a process that somehow holds more
// is answered with all it can show rather than silently trimmed to a list
// that looks complete.
func (r *simulationCaseRepository) ListByProcess(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.SimulationCase, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := simulationcase.New().
		Where(simulationcase.ProjectID.Eq(projectID), simulationcase.ProcessKey.Eq(processKey)).
		Order(simulationcase.CreatedAt.Asc(), simulationcase.ID.Asc()).
		Limit(entities.MaxSimulationCasesPerProcess+1).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the process's simulation cases: %w", err)
	}
	cases := make([]entities.SimulationCase, 0, len(rows))
	for _, row := range rows {
		c, err := caseFrom(row)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// Create records a new case, refusing one past the process's bound.
//
// The count and the insert are not one statement, so two saves at the same
// moment can both pass the count and leave the process one over. That is a
// list one case longer than intended, which the next save refuses — not worth
// a lock on every save to prevent.
func (r *simulationCaseRepository) Create(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error) {
	if c.Project == nil || c.ProcessKey == "" {
		return entities.SimulationCase{}, errors.New("a simulation case names a project and a process")
	}
	if err := r.requireProjectInTenant(ctx, c.Project.ID); err != nil {
		return entities.SimulationCase{}, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	held, err := simulationcase.New().
		Where(simulationcase.ProjectID.Eq(c.Project.ID), simulationcase.ProcessKey.Eq(c.ProcessKey)).
		Count(ctx, ex)
	if err != nil {
		return entities.SimulationCase{}, fmt.Errorf("could not count the process's simulation cases: %w", err)
	}
	if held >= entities.MaxSimulationCasesPerProcess {
		return entities.SimulationCase{}, errTooManySimulationCases
	}

	scenario, expect, err := encodeCase(c)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	ins := simulationcase.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetProjectID(c.Project.ID)
	ins.SetProcessKey(c.ProcessKey)
	ins.SetName(c.Name)
	ins.SetScenario(scenario)
	ins.SetExpect(expect)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.SimulationCase{}, fmt.Errorf("could not save the simulation case: %w", err)
	}
	return caseFrom(row)
}

// Update rewrites a case's name, scenario and expectation.
func (r *simulationCaseRepository) Update(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error) {
	row, err := r.row(ctx, c.ID)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	// A case is rewritten where it is, never moved: one named under another
	// project or process than its own is answered as not there.
	if c.Project == nil || uuid.UUID(row.ProjectID) != c.Project.ID || row.ProcessKey != c.ProcessKey {
		return entities.SimulationCase{}, fmt.Errorf("%w: this process has no such simulation case", apierr.ErrNotFound)
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	scenario, expect, err := encodeCase(c)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	mut := simulationcase.Mutate(row)
	mut.SetName(c.Name)
	mut.SetScenario(scenario)
	mut.SetExpect(expect)
	mut.SetUpdatedAtNow()
	if err := mut.Update(ctx, ex); err != nil {
		return entities.SimulationCase{}, fmt.Errorf("could not save the simulation case: %w", err)
	}
	updated, err := r.row(ctx, c.ID)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	return caseFrom(updated)
}

// Delete removes a case of the caller's.
func (r *simulationCaseRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.row(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := simulationcase.Delete(ctx, ex, id); err != nil {
		return fmt.Errorf("could not delete the simulation case: %w", err)
	}
	return nil
}

// row reads one case of the caller's. One in a project that is not theirs is
// not found, the same answer as one that does not exist.
func (r *simulationCaseRepository) row(ctx context.Context, id uuid.UUID) (simulationcase.Row, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return simulationcase.Row{}, err
	}
	row, found, err := simulationcase.New().Where(simulationcase.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return simulationcase.Row{}, fmt.Errorf("could not read the simulation case: %w", err)
	}
	if !found || r.requireProjectInTenant(ctx, row.ProjectID) != nil {
		return simulationcase.Row{}, fmt.Errorf("%w: no such simulation case", apierr.ErrNotFound)
	}
	return row, nil
}

func encodeCase(c entities.SimulationCase) (scenario, expect []byte, err error) {
	if scenario, err = json.Marshal(c.Scenario); err != nil {
		return nil, nil, fmt.Errorf("could not encode the case's scenario: %w", err)
	}
	if expect, err = json.Marshal(c.Expect); err != nil {
		return nil, nil, fmt.Errorf("could not encode the case's expectation: %w", err)
	}
	return scenario, expect, nil
}

func caseFrom(row simulationcase.Row) (entities.SimulationCase, error) {
	c := entities.SimulationCase{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		Project:    &entities.Project{ID: row.ProjectID},
		ProcessKey: row.ProcessKey,
		Name:       row.Name,
	}
	if err := json.Unmarshal(row.Scenario, &c.Scenario); err != nil {
		return entities.SimulationCase{}, fmt.Errorf("simulation case %s has a scenario that cannot be read: %w", uuid.UUID(row.ID), err)
	}
	if err := json.Unmarshal(row.Expect, &c.Expect); err != nil {
		return entities.SimulationCase{}, fmt.Errorf("simulation case %s has an expectation that cannot be read: %w", uuid.UUID(row.ID), err)
	}
	return c, nil
}
