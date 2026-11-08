package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/persondevice"
	"github.com/gsoultan/storm/runtime"
)

// tokenTailLength is how much of a device token a list shows.
const tokenTailLength = 4

type personDeviceRepository struct {
	conn  *db.Conn
	scope *conn
}

// NewPersonDeviceRepository keeps People's devices in the main database,
// beside the directory and their API keys.
//
// A person's own reads are by the participant their sign-in names. An
// administrator's are by project, and refuse a project outside their tenant.
func NewPersonDeviceRepository(c *db.Conn) contracts.PersonDeviceRepository {
	return &personDeviceRepository{conn: c, scope: &conn{conn: c}}
}

// Register upserts on the token's hash. A token is one device, so a second
// registration of it, by anybody, replaces the first: the phone changed hands,
// or another person signed in on it, and the first person must stop receiving
// pushes there.
func (r *personDeviceRepository) Register(ctx context.Context, device entities.PersonDevice, tokenHash string) (entities.PersonDevice, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.PersonDevice{}, err
	}
	ins := persondevice.Create()
	ins.SetID(device.ID)
	ins.SetProjectID(device.ProjectID)
	ins.SetParticipantID(device.ParticipantID)
	ins.SetToken(device.Token)
	ins.SetTokenHash(tokenHash)
	ins.SetPlatform(device.Platform)
	ins.SetLabel(device.Label)
	ins.SetUpdatedAt(device.CreatedAt)
	row, err := ins.OnConflictTokenHash().Insert(ctx, ex)
	if err != nil {
		return entities.PersonDevice{}, fmt.Errorf("could not record the device: %w", err)
	}
	return personDeviceFrom(row), nil
}

func (r *personDeviceRepository) ListByParticipant(ctx context.Context, participantID uuid.UUID) ([]entities.PersonDevice, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[persondevice.Row](ctx, ex, persondevice.New().
		Where(persondevice.ParticipantID.Eq(participantID)).
		Order(persondevice.CreatedAt.Desc(), persondevice.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the devices: %w", err)
	}
	return personDevicesFrom(rows), nil
}

func (r *personDeviceRepository) Remove(ctx context.Context, participantID, id uuid.UUID) error {
	return r.removeWhere(ctx, persondevice.ID.Eq(id), persondevice.ParticipantID.Eq(participantID))
}

func (r *personDeviceRepository) ListInProject(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.PersonDevice, error) {
	if err := r.scope.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[persondevice.Row](ctx, ex, persondevice.New().
		Where(persondevice.ProjectID.Eq(projectID), persondevice.ParticipantID.Eq(participantID)).
		Order(persondevice.CreatedAt.Desc(), persondevice.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the devices: %w", err)
	}
	return personDevicesFrom(rows), nil
}

func (r *personDeviceRepository) CountByProject(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	if err := r.scope.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[persondevice.Row](ctx, ex, persondevice.New().
		Where(persondevice.ProjectID.Eq(projectID)).
		Order(persondevice.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not count the devices: %w", err)
	}
	counts := make(map[uuid.UUID]int)
	for _, row := range rows {
		counts[row.ParticipantID]++
	}
	return counts, nil
}

func (r *personDeviceRepository) RemoveInProject(ctx context.Context, projectID, participantID, id uuid.UUID) error {
	if err := r.scope.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	return r.removeWhere(ctx, persondevice.ID.Eq(id), persondevice.ProjectID.Eq(projectID), persondevice.ParticipantID.Eq(participantID))
}

// removeWhere deletes the one device the predicates name, or says there is none.
func (r *personDeviceRepository) removeWhere(ctx context.Context, preds ...persondevice.Pred) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := persondevice.New().Where(preds...).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the device: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such device", apierr.ErrNotFound)
	}
	if err := persondevice.Delete(ctx, ex, row.ID); err != nil {
		return fmt.Errorf("could not remove the device: %w", err)
	}
	return nil
}

func (r *personDeviceRepository) Forget(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := persondevice.Delete(ctx, ex, id); err != nil && !errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not forget the device: %w", err)
	}
	return nil
}

func (r *personDeviceRepository) MarkSent(ctx context.Context, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	// Only the one column, keyed by id: a device removed meanwhile stays removed.
	mut := persondevice.MutateKey(id)
	mut.SetLastSentAt(at.UTC())
	if err := mut.Update(ctx, ex); err != nil && !errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not record the push: %w", err)
	}
	return nil
}

func personDevicesFrom(rows []persondevice.Row) []entities.PersonDevice {
	out := make([]entities.PersonDevice, 0, len(rows))
	for _, row := range rows {
		out = append(out, personDeviceFrom(row))
	}
	return out
}

func personDeviceFrom(row persondevice.Row) entities.PersonDevice {
	device := entities.PersonDevice{
		ID:            row.ID,
		ProjectID:     row.ProjectID,
		ParticipantID: row.ParticipantID,
		Token:         row.Token,
		Platform:      row.Platform,
		Label:         row.Label,
		CreatedAt:     row.CreatedAt,
	}
	if len(row.Token) > tokenTailLength {
		device.TokenTail = row.Token[len(row.Token)-tokenTailLength:]
	}
	if at, ok := row.LastSentAt.Get(); ok {
		device.LastSentAt = &at
	}
	return device
}
