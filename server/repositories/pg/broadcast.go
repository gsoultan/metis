package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/broadcastevent"
)

type broadcastRepository struct{ conn }

// NewBroadcastRepository returns the bus that carries live UI updates between
// replicas.
func NewBroadcastRepository(c *db.Conn) contracts.BroadcastRepository {
	return &broadcastRepository{conn{conn: c}}
}

// Publish puts one encoded event on the bus, with the audience it may reach.
//
// Deliberately outside any caller transaction. An event is published as a fact
// about something that already happened, so joining the caller's transaction
// would mean a rollback silently un-notifying browsers about work that did
// commit earlier in the same handler — and would hold the row invisible until
// commit, which is exactly when it is least useful.
func (r *broadcastRepository) Publish(ctx context.Context, origin string, scope entities.SSEScope, payload string) error {
	ex, err := r.conn.conn.Outside(ctx)
	if err != nil {
		return err
	}
	// Sealed: an event announcing a process carries its variables, and this
	// table holds them for as long as a replica might still need to catch up.
	sealed, err := sealedText(payload)
	if err != nil {
		return fmt.Errorf("could not seal the broadcast event: %w", err)
	}
	ins := broadcastevent.Create()
	ins.SetOrigin(origin)
	ins.SetPayload(sealed)
	ins.SetCreatedAt(time.Now().UTC())
	if scope.Organization != uuid.Nil {
		ins.SetOrganizationID(scope.Organization)
	}
	if scope.Environment != uuid.Nil {
		ins.SetEnvironmentID(scope.Environment)
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not publish a broadcast event: %w", err)
	}
	return nil
}

// Since returns events another replica produced, oldest first.
//
// A replica delivers its own events to its own clients directly, so it excludes
// them here rather than delivering each one twice.
func (r *broadcastRepository) Since(ctx context.Context, origin string, afterID int64, limit int) ([]models.BroadcastEventModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := broadcastevent.New().
		Where(broadcastevent.ID.Gt(afterID), broadcastevent.Origin.NotEq(origin)).
		Order(broadcastevent.ID.Asc()).
		Limit(int64(limit)).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read broadcast events: %w", err)
	}
	out := make([]models.BroadcastEventModel, 0, len(rows))
	for _, row := range rows {
		payload, err := openedText(row.Payload)
		if err != nil {
			return nil, fmt.Errorf("could not open broadcast event %d: %w", row.ID, err)
		}
		event := models.BroadcastEventModel{
			ID:        row.ID,
			Origin:    row.Origin,
			Payload:   payload,
			CreatedAt: row.CreatedAt,
		}
		if organization, ok := row.OrganizationID.Get(); ok {
			id := models.UUID(organization)
			event.OrganizationID = &id
		}
		if environment, ok := row.EnvironmentID.Get(); ok {
			id := models.UUID(environment)
			event.EnvironmentID = &id
		}
		out = append(out, event)
	}
	return out, nil
}

// LatestID is where a replica starts reading.
//
// The current end rather than zero: replaying history would flood every browser
// that connected after a restart with invalidations for work that finished
// before they arrived.
func (r *broadcastRepository) LatestID(ctx context.Context) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	row, found, err := broadcastevent.New().
		Order(broadcastevent.ID.Desc()).
		Limit(1).
		One(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not read the latest broadcast id: %w", err)
	}
	if !found {
		// An empty table is the ordinary state of a fresh installation, not a
		// condition to handle.
		return 0, nil
	}
	return row.ID, nil
}

// Prune deletes events older than the cutoff.
//
// The table is a bus, not a log — that is what the audit trail is for — so a row
// every live replica has moved past has no readers left.
func (r *broadcastRepository) Prune(ctx context.Context, olderThan time.Time) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := ex.Exec(ctx,
		"DELETE FROM broadcast_events WHERE created_at < $1", []any{olderThan.UTC()})
	if err != nil {
		return 0, fmt.Errorf("could not prune broadcast events: %w", err)
	}
	return removed, nil
}
