package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/environment"
	"github.com/gsoultan/storm/runtime"
)

type environmentRepository struct{ conn }

// NewEnvironmentRepository returns the registry of a project's runtimes.
func NewEnvironmentRepository(c *db.Conn) contracts.EnvironmentRepository {
	return &environmentRepository{conn{conn: c}}
}

func (r *environmentRepository) Get(ctx context.Context, id uuid.UUID) (models.EnvironmentModel, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return models.EnvironmentModel{}, err
	}
	row, found, err := environment.New().Where(environment.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return models.EnvironmentModel{}, fmt.Errorf("could not read the environment: %w", err)
	}
	if !found {
		return models.EnvironmentModel{}, fmt.Errorf("%w: no such environment", apierr.ErrNotFound)
	}
	if err := r.requireProjectInTenant(ctx, uuid.UUID(row.ProjectID)); err != nil {
		return models.EnvironmentModel{}, fmt.Errorf("%w: no such environment", apierr.ErrNotFound)
	}
	return environmentFrom(row)
}

func (r *environmentRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.EnvironmentModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	q := environment.New().Order(environment.Name.Asc())
	if scoped != nil {
		q = q.Where(environment.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list environments: %w", err)
	}
	return environmentsFrom(rows)
}

// ListAll returns every environment in the installation.
//
// Unscoped by design: the caller is the environment watcher, which opens each
// runtime's database and binds it to a port, at boot and at every check after.
// A tenant-scoped answer there would serve only whichever organization happened
// to be on the context, which for background work is none.
func (r *environmentRepository) ListAll(ctx context.Context) ([]models.EnvironmentModel, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := environment.New().Order(environment.Port.Asc()).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list environments: %w", err)
	}
	return environmentsFrom(rows)
}

func (r *environmentRepository) Create(ctx context.Context, e models.EnvironmentModel) error {
	projectID := uuid.UUID(e.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	connection, err := e.Connection.Value()
	if err != nil {
		return fmt.Errorf("could not encrypt the environment's connection: %w", err)
	}
	ins := environment.Create()
	if id := uuid.UUID(e.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetName(e.Name)
	ins.SetPort(int64(e.Port))
	ins.SetDriver(e.Driver)
	ins.SetConnection(stringOfValue(connection))
	ins.SetEnabled(e.Enabled)
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: that name or port is already taken", apierr.ErrInvalidArgument)
		}
		return fmt.Errorf("could not create the environment: %w", err)
	}
	return nil
}

func (r *environmentRepository) Update(ctx context.Context, e models.EnvironmentModel) error {
	id := uuid.UUID(e.ID)
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := environment.New().Where(environment.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the environment: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such environment", apierr.ErrNotFound)
	}
	connection, err := e.Connection.Value()
	if err != nil {
		return fmt.Errorf("could not encrypt the environment's connection: %w", err)
	}
	mut := environment.Mutate(row)
	mut.SetName(e.Name)
	mut.SetPort(int64(e.Port))
	mut.SetDriver(e.Driver)
	mut.SetConnection(stringOfValue(connection))
	mut.SetEnabled(e.Enabled)
	if err := mut.Update(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: that name or port is already taken", apierr.ErrInvalidArgument)
		}
		return fmt.Errorf("could not update the environment: %w", err)
	}
	return nil
}

// Delete removes a runtime from the registry.
//
// The database it named is untouched. Removing the row stops the runtime being
// served; destroying what it ran is a separate act, and not one a delete button
// on a settings page should perform.
func (r *environmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := environment.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such environment", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the environment: %w", err)
	}
	return nil
}

// PortTaken reports whether another environment already holds a port.
//
// Installation-wide, not per tenant: two environments cannot both bind 8081
// whoever owns them, and a check that only saw the caller's own would refuse to
// notice the collision until every replica failed to bind it.
func (r *environmentRepository) PortTaken(ctx context.Context, port int, excluding uuid.UUID) (bool, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return false, err
	}
	q := environment.New().Where(environment.Port.Eq(int64(port)))
	if excluding != uuid.Nil {
		q = q.Where(environment.ID.NotEq(excluding))
	}
	taken, err := q.Exists(ctx, ex)
	if err != nil {
		return false, fmt.Errorf("could not check the port: %w", err)
	}
	return taken, nil
}

func environmentsFrom(rows []environment.Row) ([]models.EnvironmentModel, error) {
	out := make([]models.EnvironmentModel, 0, len(rows))
	for _, row := range rows {
		e, err := environmentFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func environmentFrom(row environment.Row) (models.EnvironmentModel, error) {
	var connection models.EncryptedMap
	if row.Connection != "" {
		if err := connection.Scan(row.Connection); err != nil {
			return models.EnvironmentModel{}, fmt.Errorf("could not decrypt the environment's connection: %w", err)
		}
	}
	return models.EnvironmentModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:  models.UUID(row.ProjectID),
		Name:       row.Name,
		Port:       int(row.Port),
		Driver:     row.Driver,
		Connection: connection,
		Enabled:    row.Enabled,
	}, nil
}
