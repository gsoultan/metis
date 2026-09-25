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
	"github.com/gsoultan/metis/server/repositories/store/subscription"
	"github.com/gsoultan/storm/runtime"
)

type subscriptionRepository struct{ conn }

// NewSubscriptionRepository returns the store of what a process is waiting for.
func NewSubscriptionRepository(c *db.Conn) contracts.SubscriptionRepository {
	return &subscriptionRepository{conn{conn: c}}
}

// Create records that an instance is waiting on a signal or a message.
//
// Written by the engine as it parks a token, so it is not scoped: a refusal
// would leave an instance waiting for something nothing will ever deliver.
func (r *subscriptionRepository) Create(ctx context.Context, sub models.Subscription) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := subscription.Create()
	if id := uuid.UUID(sub.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(uuid.UUID(sub.ProjectID))
	ins.SetInstanceID(uuid.UUID(sub.InstanceID))
	ins.SetNodeID(sub.NodeID)
	ins.SetType(string(sub.Type))
	ins.SetEventName(sub.EventName)
	setOrNullString(ins.SetCorrelationKey, ins.SetCorrelationKeyNull, sub.CorrelationKey)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the subscription: %w", err)
	}
	return nil
}

// Delete drops one subscription, refusing another tenant's.
//
// Read within scope first: a delete that checked afterwards would have already
// deleted the row it was refusing.
func (r *subscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := subscription.New().Where(subscription.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the subscription: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such subscription", apierr.ErrNotFound)
	}
	if err := r.requireProjectInTenant(ctx, uuid.UUID(row.ProjectID)); err != nil {
		return fmt.Errorf("%w: no such subscription", apierr.ErrNotFound)
	}
	if err := subscription.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such subscription", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the subscription: %w", err)
	}
	return nil
}

// DeleteByNode drops what one node of one instance was waiting for.
//
// Raw SQL: this deletes by a pair that is not the primary key, and reading the
// rows first to name them would be a read of exactly the rows about to go.
func (r *subscriptionRepository) DeleteByNode(ctx context.Context, instanceID uuid.UUID, nodeID string) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if _, err := ex.Exec(ctx,
		"DELETE FROM event_subscriptions WHERE instance_id = $1 AND node_id = $2",
		[]any{instanceID, nodeID}); err != nil {
		return fmt.Errorf("could not clear the node's subscriptions: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.Subscription, error) {
	return r.list(ctx, subscription.InstanceID.Eq(instanceID))
}

// FindSignals returns everything in a project waiting on a signal name.
//
// A signal is broadcast: every instance waiting on that name in that project is
// woken, which is what makes it a signal rather than a message.
func (r *subscriptionRepository) FindSignals(ctx context.Context, projectID uuid.UUID, signalName string) ([]models.Subscription, error) {
	return r.list(ctx,
		subscription.ProjectID.Eq(projectID),
		subscription.Type.Eq(string(models.SubscriptionSignal)),
		subscription.EventName.Eq(signalName),
	)
}

// FindMessages returns what a message would be delivered to.
//
// An empty correlation key matches every subscription for that message name,
// which is the documented behaviour and the reason a message naming nothing
// succeeds silently: the caller asked to wake whoever is waiting, and nobody
// was.
func (r *subscriptionRepository) FindMessages(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string) ([]models.Subscription, error) {
	preds := []subscription.Pred{
		subscription.ProjectID.Eq(projectID),
		subscription.Type.Eq(string(models.SubscriptionMessage)),
		subscription.EventName.Eq(messageName),
	}
	if correlationKey != "" {
		preds = append(preds, subscription.CorrelationKey.Eq(correlationKey))
	}
	return r.list(ctx, preds...)
}

// ListTemplatedMessageSubscriptions returns the subscriptions whose correlation
// key still holds a ${...} placeholder.
//
// Unscoped, because it is the backfill's input: a key written before templates
// were resolved per instance is stale wherever it is, and finding only one
// tenant's would leave the rest permanently unmatched.
func (r *subscriptionRepository) ListTemplatedMessageSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := subscription.New().
		Where(
			subscription.Type.Eq(string(models.SubscriptionMessage)),
			subscription.CorrelationKey.Like("%${%"),
		).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the templated subscriptions: %w", err)
	}
	return subscriptionsFrom(rows), nil
}

// UpdateCorrelationKey resolves a template against the instance it belongs to.
func (r *subscriptionRepository) UpdateCorrelationKey(ctx context.Context, id uuid.UUID, correlationKey string) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := subscription.New().Where(subscription.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the subscription: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such subscription", apierr.ErrNotFound)
	}
	if err := r.requireProjectInTenant(ctx, uuid.UUID(row.ProjectID)); err != nil {
		return err
	}
	mut := subscription.Mutate(row)
	setOrNullString(mut.SetCorrelationKey, mut.SetCorrelationKeyNull, correlationKey)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the correlation key: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) list(ctx context.Context, preds ...subscription.Pred) ([]models.Subscription, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := subscription.New().Where(preds...)
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return nil, nil
		}
		q = q.Where(subscription.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	// Every row, not the store's first thousand: a signal is owed to every
	// instance waiting for it, and a migration moves every one an instance has.
	rows, err := everyRow[subscription.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not read subscriptions: %w", err)
	}
	return subscriptionsFrom(rows), nil
}

func subscriptionsFrom(rows []subscription.Row) []models.Subscription {
	out := make([]models.Subscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.Subscription{
			Base: models.Base{
				ID:        models.UUID(row.ID),
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			ProjectID:      models.UUID(row.ProjectID),
			InstanceID:     models.UUID(row.InstanceID),
			NodeID:         row.NodeID,
			Type:           models.SubscriptionType(row.Type),
			EventName:      row.EventName,
			CorrelationKey: valueOr(row.CorrelationKey),
		})
	}
	return out
}
