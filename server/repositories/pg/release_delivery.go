package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/releasedelivery"
	"github.com/gsoultan/metis/server/repositories/store/serverconnection"
	"github.com/gsoultan/storm/runtime"
)

// maxReleaseDeliveries bounds the deliveries read at once: a project's recent
// releases to a few servers, or what one server has not settled.
const maxReleaseDeliveries = 200

func (r *serverConnectionRepository) GetDownstreamByTokenHash(ctx context.Context, hash string) (entities.ServerConnection, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	row, found, err := serverconnection.New().
		Where(serverconnection.TokenHash.Eq(hash),
			serverconnection.Side.Eq(string(entities.ConnectionDownstream))).
		One(ctx, ex)
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not read the connections: %w", err)
	}
	if !found || hash == "" {
		return entities.ServerConnection{}, fmt.Errorf("%w: no such connection", apierr.ErrNotFound)
	}
	return serverConnectionFrom(row, false)
}

func (r *serverConnectionRepository) Seen(ctx context.Context, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	var seen serverconnection.Mut
	seen.SetLastSeenAt(at.UTC())
	if _, err := serverconnection.New().Where(serverconnection.ID.Eq(id)).UpdateAll(ctx, ex, seen); err != nil {
		return fmt.Errorf("could not record when the server was seen: %w", err)
	}
	return nil
}

func (r *serverConnectionRepository) SendRelease(ctx context.Context, d entities.ReleaseDelivery) (entities.ReleaseDelivery, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ReleaseDelivery{}, err
	}
	same := releasedelivery.New().Where(
		releasedelivery.ConnectionID.Eq(d.ConnectionID.String()),
		releasedelivery.ProjectID.Eq(d.ProjectID.String()),
		releasedelivery.Release.Eq(d.Release))
	row, found, err := same.One(ctx, ex)
	if err != nil {
		return entities.ReleaseDelivery{}, fmt.Errorf("could not read the releases sent: %w", err)
	}
	if found && entities.DeliveryState(row.State) != entities.DeliveryRefused {
		return releaseDeliveryFrom(row), nil
	}
	if found {
		// Refused there, perhaps for something since put right there: send it
		// again, as new. Only a refused one, so two people sending at once
		// send it once.
		var again releasedelivery.Mut
		again.SetState(string(entities.DeliveryWaiting))
		again.SetDetail("")
		again.SetSentBy(d.SentBy)
		again.SetSentAt(d.SentAt.UTC())
		again.SetCollectedAtNull()
		again.SetReportedAtNull()
		if _, err := same.Where(releasedelivery.State.Eq(string(entities.DeliveryRefused))).UpdateAll(ctx, ex, again); err != nil {
			return entities.ReleaseDelivery{}, fmt.Errorf("could not send the release again: %w", err)
		}
	} else {
		ins := releasedelivery.Create()
		ins.SetID(uuid.Must(uuid.NewV7()))
		ins.SetConnectionID(d.ConnectionID.String())
		ins.SetProjectID(d.ProjectID.String())
		ins.SetRelease(d.Release)
		ins.SetState(string(entities.DeliveryWaiting))
		ins.SetDetail("")
		ins.SetSentBy(d.SentBy)
		ins.SetSentAt(d.SentAt.UTC())
		// Two people sending the same release at once: the second finds the
		// first's row.
		ins.OnConflictConnectionIDProjectIDRelease().DoNothing()
		if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
			return entities.ReleaseDelivery{}, fmt.Errorf("could not record the release sent: %w", err)
		}
	}
	row, found, err = same.One(ctx, ex)
	if err != nil || !found {
		return entities.ReleaseDelivery{}, fmt.Errorf("could not read the release sent back: %w", err)
	}
	return releaseDeliveryFrom(row), nil
}

func (r *serverConnectionRepository) ListDeliveries(ctx context.Context, projectID uuid.UUID) ([]entities.ReleaseDelivery, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := releasedelivery.New().
		Where(releasedelivery.ProjectID.Eq(projectID.String())).
		Order(releasedelivery.SentAt.Desc(), releasedelivery.ID.Desc()).
		Limit(maxReleaseDeliveries).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the releases sent: %w", err)
	}
	return releaseDeliveriesFrom(rows), nil
}

func (r *serverConnectionRepository) OpenDeliveries(ctx context.Context, connectionID uuid.UUID) ([]entities.ReleaseDelivery, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := releasedelivery.New().
		Where(releasedelivery.ConnectionID.Eq(connectionID.String()),
			releasedelivery.State.NotIn(
				string(entities.DeliveryLive),
				string(entities.DeliverySuperseded),
				string(entities.DeliveryRefused))).
		Order(releasedelivery.SentAt.Asc(), releasedelivery.ID.Asc()).
		Limit(maxReleaseDeliveries).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the releases sent: %w", err)
	}
	return releaseDeliveriesFrom(rows), nil
}

func (r *serverConnectionRepository) MarkCollected(ctx context.Context, connectionID uuid.UUID, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	raw := make([][16]byte, len(ids))
	for i, id := range ids {
		raw[i] = id
	}
	var collected releasedelivery.Mut
	collected.SetCollectedAt(at.UTC())
	if _, err := releasedelivery.New().
		Where(releasedelivery.ConnectionID.Eq(connectionID.String()),
			releasedelivery.ID.In(raw...),
			releasedelivery.CollectedAt.IsNull()).
		UpdateAll(ctx, ex, collected); err != nil {
		return fmt.Errorf("could not record the releases collected: %w", err)
	}
	return nil
}

func (r *serverConnectionRepository) ReportDelivery(ctx context.Context, connectionID, id uuid.UUID, state entities.DeliveryState, detail string, at time.Time) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	var reported releasedelivery.Mut
	reported.SetState(string(state))
	reported.SetDetail(detail)
	reported.SetReportedAt(at.UTC())
	if _, err := releasedelivery.New().
		Where(releasedelivery.ConnectionID.Eq(connectionID.String()), releasedelivery.ID.Eq(id)).
		UpdateAll(ctx, ex, reported); err != nil {
		return fmt.Errorf("could not record what the server said: %w", err)
	}
	return nil
}

func releaseDeliveriesFrom(rows []releasedelivery.Row) []entities.ReleaseDelivery {
	out := make([]entities.ReleaseDelivery, 0, len(rows))
	for _, row := range rows {
		out = append(out, releaseDeliveryFrom(row))
	}
	return out
}

func releaseDeliveryFrom(row releasedelivery.Row) entities.ReleaseDelivery {
	d := entities.ReleaseDelivery{
		ID:           row.ID,
		ConnectionID: uuid.MustParse(row.ConnectionID),
		ProjectID:    uuid.MustParse(row.ProjectID),
		Release:      row.Release,
		State:        entities.DeliveryState(row.State),
		Detail:       row.Detail,
		SentBy:       row.SentBy,
		SentAt:       row.SentAt,
	}
	if at, ok := row.CollectedAt.Get(); ok {
		d.CollectedAt = &at
	}
	if at, ok := row.ReportedAt.Get(); ok {
		d.ReportedAt = &at
	}
	return d
}
