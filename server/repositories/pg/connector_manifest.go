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
	"github.com/gsoultan/metis/server/repositories/store/connectormanifest"
	"github.com/gsoultan/storm/runtime"
)

type connectorManifestRepository struct{ conn }

// NewConnectorManifestRepository returns the installed connector manifests.
//
// Installation-wide rather than per project: a manifest describes a kind of
// connector, and the instances that configure one are what belong to a project.
// So nothing here is tenant-scoped, and every write is administrative — the
// endpoints are gated accordingly.
func NewConnectorManifestRepository(c *db.Conn) contracts.ConnectorManifestRepository {
	return &connectorManifestRepository{conn{conn: c}}
}

func (r *connectorManifestRepository) GetByKey(ctx context.Context, key string) (models.ConnectorManifestModel, error) {
	return r.byKey(ctx, key, false)
}

func (r *connectorManifestRepository) GetByKeyForUpdate(ctx context.Context, key string) (models.ConnectorManifestModel, error) {
	return r.byKey(ctx, key, true)
}

func (r *connectorManifestRepository) byKey(ctx context.Context, key string, forUpdate bool) (models.ConnectorManifestModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ConnectorManifestModel{}, err
	}
	q := connectormanifest.New().Where(connectormanifest.Key.Eq(key))
	if forUpdate {
		q = q.ForUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return models.ConnectorManifestModel{}, fmt.Errorf("could not read the connector manifest: %w", err)
	}
	if !found {
		return models.ConnectorManifestModel{}, fmt.Errorf("%w: no manifest for %q", apierr.ErrNotFound, key)
	}
	return manifestFrom(row), nil
}

func (r *connectorManifestRepository) List(ctx context.Context) ([]models.ConnectorManifestModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := connectormanifest.New().Order(connectormanifest.Key.Asc()).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list connector manifests: %w", err)
	}
	out := make([]models.ConnectorManifestModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, manifestFrom(row))
	}
	return out, nil
}

// Upsert installs a manifest, replacing the one already under that key.
//
// Keyed rather than versioned-append: installing version 3 of a connector
// replaces version 2, because the instances configured against that key run
// whatever is installed. Keeping both would mean two answers to "what does this
// connector do" with nothing choosing between them.
func (r *connectorManifestRepository) Upsert(ctx context.Context, manifest models.ConnectorManifestModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := connectormanifest.Create()
	if id := uuid.UUID(manifest.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetKey(manifest.Key)
	ins.SetName(manifest.Name)
	ins.SetVersion(int64(manifest.Version))
	ins.SetDocument(manifest.Document)
	ins.SetEnabled(manifest.Enabled)
	// Reinstating one that was removed: installing it again is somebody saying
	// they want it back, and the key is scoped to the live rows so a marked row
	// would otherwise sit there invisible while the insert made a second.
	ins.SetDeletedAtNull()
	ins.OnConflictKey()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not install the connector manifest: %w", err)
	}
	return nil
}

// SetEnabled stops or restarts a connector without uninstalling it, so the
// instances configured against it keep their configuration.
func (r *connectorManifestRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := connectormanifest.New().Where(connectormanifest.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the connector manifest: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such connector manifest", apierr.ErrNotFound)
	}
	mut := connectormanifest.Mutate(row)
	mut.SetEnabled(enabled)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not change the connector manifest: %w", err)
	}
	return nil
}

func (r *connectorManifestRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := connectormanifest.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such connector manifest", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the connector manifest: %w", err)
	}
	return nil
}

func manifestFrom(row connectormanifest.Row) models.ConnectorManifestModel {
	return models.ConnectorManifestModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		Key:      row.Key,
		Name:     row.Name,
		Version:  int(row.Version),
		Document: row.Document,
		Enabled:  row.Enabled,
	}
}
