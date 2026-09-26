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
	"github.com/gsoultan/metis/server/repositories/store/connector"
	"github.com/gsoultan/metis/server/repositories/store/connectorinstance"
	"github.com/gsoultan/storm/runtime"
)

type connectorRepository struct{ conn }

// NewConnectorRepository returns the catalogue of connector kinds.
//
// Installation-wide, like the manifests: a connector describes what a kind of
// integration needs, and what belongs to a project is the instance that
// configures one. Nothing here is tenant-scoped and every write is
// administrative.
func NewConnectorRepository(c *db.Conn) contracts.ConnectorRepository {
	return &connectorRepository{conn{conn: c}}
}

func (r *connectorRepository) List(ctx context.Context) ([]models.Connector, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := connector.New().Order(connector.Name.Asc()).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list connectors: %w", err)
	}
	out := make([]models.Connector, 0, len(rows))
	for _, row := range rows {
		c, err := connectorFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *connectorRepository) Get(ctx context.Context, id uuid.UUID) (models.Connector, error) {
	return r.one(ctx, connector.ID.Eq(id))
}

func (r *connectorRepository) GetByKey(ctx context.Context, key string) (models.Connector, error) {
	return r.one(ctx, connector.Key.Eq(key))
}

func (r *connectorRepository) Create(ctx context.Context, c models.Connector) (models.Connector, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.Connector{}, err
	}
	properties, err := propertiesOf(c.Schema)
	if err != nil {
		return models.Connector{}, err
	}
	ins := connector.Create()
	if id := uuid.UUID(c.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetKey(c.Key)
	ins.SetName(c.Name)
	ins.SetType(c.Type)
	ins.SetProperties(properties)
	setOrNullString(ins.SetDescription, ins.SetDescriptionNull, c.Description)
	setOrNullString(ins.SetIcon, ins.SetIconNull, c.Icon)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return models.Connector{}, fmt.Errorf("%w: a connector called %q already exists", apierr.ErrInvalidArgument, c.Key)
		}
		return models.Connector{}, fmt.Errorf("could not create the connector: %w", err)
	}
	return connectorFrom(row)
}

func (r *connectorRepository) Update(ctx context.Context, c models.Connector) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := connector.New().Where(connector.ID.Eq(uuid.UUID(c.ID))).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the connector: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such connector", apierr.ErrNotFound)
	}
	properties, err := propertiesOf(c.Schema)
	if err != nil {
		return err
	}
	mut := connector.Mutate(row)
	mut.SetKey(c.Key)
	mut.SetName(c.Name)
	mut.SetType(c.Type)
	mut.SetProperties(properties)
	setOrNullString(mut.SetDescription, mut.SetDescriptionNull, c.Description)
	setOrNullString(mut.SetIcon, mut.SetIconNull, c.Icon)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the connector: %w", err)
	}
	return nil
}

func (r *connectorRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := connector.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such connector", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the connector: %w", err)
	}
	return nil
}

// restoreConnectorByKeySQL clears the removal mark on the entry removed last
// under a key and returns it. Raw SQL because every generated read hides
// removed rows, and this has to find one by its key.
const restoreConnectorByKeySQL = `UPDATE "connectors" SET "deleted_at" = NULL, "updated_at" = now()
WHERE "id" = (SELECT "id" FROM "connectors" WHERE "key" = $1 AND "deleted_at" IS NOT NULL
	ORDER BY "deleted_at" DESC LIMIT 1)
RETURNING "id", "created_at", "updated_at", "key", "name", "description", "icon", "type", "properties", "deleted_at"`

func (r *connectorRepository) RestoreByKey(ctx context.Context, key string) (models.Connector, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.Connector{}, err
	}
	rows, err := ex.Query(ctx, restoreConnectorByKeySQL, []any{key})
	if err != nil {
		return models.Connector{}, fmt.Errorf("could not restore the connector: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		// pgx reports a failed statement when the rows are drained, not when
		// the query is sent, so an empty result is only "none" once Err says so.
		if err := rows.Err(); err != nil {
			return models.Connector{}, fmt.Errorf("could not restore the connector: %w", err)
		}
		return models.Connector{}, fmt.Errorf("%w: no connector called %q was removed", apierr.ErrNotFound, key)
	}
	var row connector.Row
	var slab runtime.Slab
	if err := connector.Scan(rows.RawValues(), &row, &slab); err != nil {
		return models.Connector{}, fmt.Errorf("could not read the restored connector: %w", err)
	}
	return connectorFrom(row)
}

func (r *connectorRepository) one(ctx context.Context, preds ...connector.Pred) (models.Connector, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.Connector{}, err
	}
	row, found, err := connector.New().Where(preds...).One(ctx, ex)
	if err != nil {
		return models.Connector{}, fmt.Errorf("could not read the connector: %w", err)
	}
	if !found {
		return models.Connector{}, fmt.Errorf("%w: no such connector", apierr.ErrNotFound)
	}
	return connectorFrom(row)
}

func propertiesOf(schema []models.ConnectorProperty) (runtime.JSON, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("could not encode the connector's properties: %w", err)
	}
	return encoded, nil
}

func connectorFrom(row connector.Row) (models.Connector, error) {
	c := models.Connector{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		Key:         row.Key,
		Name:        row.Name,
		Description: valueOr(row.Description),
		Icon:        valueOr(row.Icon),
		Type:        row.Type,
	}
	if len(row.Properties) > 0 {
		if err := json.Unmarshal(row.Properties, &c.Schema); err != nil {
			return models.Connector{}, fmt.Errorf("could not decode a connector's properties: %w", err)
		}
	}
	return c, nil
}

type connectorInstanceRepository struct{ conn }

// NewConnectorInstanceRepository returns a project's configured connectors.
//
// This is where the credentials are, so every read and write is tenant-scoped
// and the configuration is encrypted at rest — see models.EncryptedMap, which
// does the encrypting so that a repository on either ORM writes the same bytes.
func NewConnectorInstanceRepository(c *db.Conn) contracts.ConnectorInstanceRepository {
	return &connectorInstanceRepository{conn{conn: c}}
}

func (r *connectorInstanceRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ConnectorInstance, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := connectorinstance.New().Order(connectorinstance.Name.Asc())
	if scoped != nil {
		q = q.Where(connectorinstance.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list connector instances: %w", err)
	}
	out := make([]models.ConnectorInstance, 0, len(rows))
	for _, row := range rows {
		instance, err := connectorInstanceFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, instance)
	}
	return out, nil
}

func (r *connectorInstanceRepository) Get(ctx context.Context, id uuid.UUID) (models.ConnectorInstance, error) {
	return r.one(ctx, connectorinstance.ID.Eq(id))
}

func (r *connectorInstanceRepository) GetByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (models.ConnectorInstance, error) {
	return r.one(ctx,
		connectorinstance.ProjectID.Eq(projectID),
		connectorinstance.ConnectorID.Eq(connectorID),
	)
}

func (r *connectorInstanceRepository) Create(ctx context.Context, instance models.ConnectorInstance) (models.ConnectorInstance, error) {
	projectID := uuid.UUID(instance.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return models.ConnectorInstance{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ConnectorInstance{}, err
	}
	config, err := instance.Config.Value()
	if err != nil {
		return models.ConnectorInstance{}, fmt.Errorf("could not encrypt the connector's configuration: %w", err)
	}
	ins := connectorinstance.Create()
	if id := uuid.UUID(instance.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetConnectorID(uuid.UUID(instance.ConnectorID))
	ins.SetName(instance.Name)
	ins.SetConfig(stringOfValue(config))
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return models.ConnectorInstance{}, fmt.Errorf("could not create the connector instance: %w", err)
	}
	return connectorInstanceFrom(row)
}

func (r *connectorInstanceRepository) Update(ctx context.Context, instance models.ConnectorInstance) error {
	id := uuid.UUID(instance.ID)
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := connectorinstance.New().Where(connectorinstance.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the connector instance: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such connector instance", apierr.ErrNotFound)
	}
	config, err := instance.Config.Value()
	if err != nil {
		return fmt.Errorf("could not encrypt the connector's configuration: %w", err)
	}
	mut := connectorinstance.Mutate(row)
	mut.SetName(instance.Name)
	mut.SetConfig(stringOfValue(config))
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the connector instance: %w", err)
	}
	return nil
}

func (r *connectorInstanceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := connectorinstance.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such connector instance", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the connector instance: %w", err)
	}
	return nil
}

func (r *connectorInstanceRepository) one(ctx context.Context, preds ...connectorinstance.Pred) (models.ConnectorInstance, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return models.ConnectorInstance{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ConnectorInstance{}, err
	}
	q := connectorinstance.New().Where(preds...)
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return models.ConnectorInstance{}, fmt.Errorf("%w: no such connector instance", apierr.ErrNotFound)
		}
		q = q.Where(connectorinstance.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return models.ConnectorInstance{}, fmt.Errorf("could not read the connector instance: %w", err)
	}
	if !found {
		return models.ConnectorInstance{}, fmt.Errorf("%w: no such connector instance", apierr.ErrNotFound)
	}
	return connectorInstanceFrom(row)
}

func connectorInstanceFrom(row connectorinstance.Row) (models.ConnectorInstance, error) {
	var config models.EncryptedMap
	if row.Config != "" {
		if err := config.Scan(row.Config); err != nil {
			return models.ConnectorInstance{}, fmt.Errorf("could not decrypt the connector's configuration: %w", err)
		}
	}
	return models.ConnectorInstance{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:   models.UUID(row.ProjectID),
		ConnectorID: models.UUID(row.ConnectorID),
		Name:        row.Name,
		Config:      config,
	}, nil
}
