package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/outgoingwebhook"
	"github.com/gsoultan/storm/runtime"
)

type outgoingWebhookRepository struct{ conn }

// NewOutgoingWebhookRepository returns the store of the addresses a project
// posts its events to.
func NewOutgoingWebhookRepository(c *db.Conn) contracts.OutgoingWebhookRepository {
	return &outgoingWebhookRepository{conn{conn: c}}
}

// Create records a webhook with its secret sealed.
//
// Sealed rather than stored as given: the secret is what a receiver trusts a
// delivery by, so a copy of this table must not be enough to forge one.
// Without a key the write is refused rather than made in the clear.
func (r *outgoingWebhookRepository) Create(ctx context.Context, w entities.OutgoingWebhook) (entities.OutgoingWebhook, error) {
	if w.Project == nil {
		return entities.OutgoingWebhook{}, errors.New("an outgoing webhook belongs to a project")
	}
	if err := r.requireProjectInTenant(ctx, w.Project.ID); err != nil {
		return entities.OutgoingWebhook{}, err
	}
	sealed, err := crypto.Encrypt(w.Secret)
	if err != nil {
		return entities.OutgoingWebhook{}, fmt.Errorf("could not seal the webhook's secret: %w", err)
	}
	events, err := json.Marshal(w.Events)
	if err != nil {
		return entities.OutgoingWebhook{}, fmt.Errorf("could not record the webhook's events: %w", err)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	ins := outgoingwebhook.Create()
	if w.ID != uuid.Nil {
		ins.SetID(w.ID)
	}
	ins.SetProjectID(w.Project.ID)
	ins.SetName(w.Name)
	ins.SetURL(w.URL)
	ins.SetEvents(events)
	ins.SetSecret(sealed)
	ins.SetEnabled(w.Enabled)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.OutgoingWebhook{}, fmt.Errorf("could not create the outgoing webhook: %w", err)
	}
	created, err := outgoingWebhookFrom(row)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	created.Secret = w.Secret
	return created, nil
}

func (r *outgoingWebhookRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]entities.OutgoingWebhook, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := outgoingwebhook.New().Order(outgoingwebhook.CreatedAt.Asc(), outgoingwebhook.ID.Asc())
	if scoped != nil {
		q = q.Where(outgoingwebhook.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the outgoing webhooks: %w", err)
	}
	out := make([]entities.OutgoingWebhook, 0, len(rows))
	for _, row := range rows {
		w, err := outgoingWebhookFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

func (r *outgoingWebhookRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	row, err := r.one(ctx, id)
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := outgoingwebhook.Mutate(row)
	mut.SetEnabled(enabled)
	mut.SetUpdatedAtNow()
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not change the outgoing webhook: %w", err)
	}
	return nil
}

func (r *outgoingWebhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.one(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := outgoingwebhook.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return errNoOutgoingWebhook
		}
		return fmt.Errorf("could not delete the outgoing webhook: %w", err)
	}
	return nil
}

var errNoOutgoingWebhook = fmt.Errorf("%w: no such outgoing webhook", apierr.ErrNotFound)

// one reads a webhook within the caller's scope.
func (r *outgoingWebhookRepository) one(ctx context.Context, id uuid.UUID) (outgoingwebhook.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return outgoingwebhook.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return outgoingwebhook.Row{}, err
	}
	q := outgoingwebhook.New().Where(outgoingwebhook.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return outgoingwebhook.Row{}, errNoOutgoingWebhook
		}
		q = q.Where(outgoingwebhook.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return outgoingwebhook.Row{}, fmt.Errorf("could not read the outgoing webhook: %w", err)
	}
	if !found {
		return outgoingwebhook.Row{}, errNoOutgoingWebhook
	}
	return row, nil
}

// outgoingWebhookFrom maps a row to the entity, without its secret.
func outgoingWebhookFrom(row outgoingwebhook.Row) (entities.OutgoingWebhook, error) {
	var events []entities.OutgoingWebhookEvent
	if len(row.Events) > 0 {
		if err := json.Unmarshal(row.Events, &events); err != nil {
			return entities.OutgoingWebhook{}, fmt.Errorf("could not read the events of outgoing webhook %s: %w", uuid.UUID(row.ID), err)
		}
	}
	return entities.OutgoingWebhook{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		Project:   &entities.Project{ID: row.ProjectID},
		Name:      row.Name,
		URL:       row.URL,
		Events:    events,
		Enabled:   row.Enabled,
	}, nil
}
