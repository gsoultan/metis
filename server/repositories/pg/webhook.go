package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/webhook"
	"github.com/gsoultan/metis/server/repositories/store/webhookdelivery"
	"github.com/gsoultan/storm/runtime"
)

type webhookRepository struct{ conn }

// NewWebhookRepository returns the store of inbound webhooks.
func NewWebhookRepository(c *db.Conn) contracts.WebhookRepository {
	return &webhookRepository{conn{conn: c}}
}

// GetByToken finds the webhook a request presented a token for.
//
// Unscoped, and it has to be: the caller is an outside system holding a token
// and nothing else. The token is the credential — unique across the deleted
// rows, so a removed webhook's token is never reissued to a different endpoint
// — and what it resolves to decides the tenant for everything after.
func (r *webhookRepository) GetByToken(ctx context.Context, token string) (models.WebhookModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.WebhookModel{}, err
	}
	row, found, err := webhook.New().Where(webhook.Token.Eq(token)).One(ctx, ex)
	if err != nil {
		return models.WebhookModel{}, fmt.Errorf("could not read the webhook: %w", err)
	}
	if !found {
		return models.WebhookModel{}, fmt.Errorf("%w: no such webhook", apierr.ErrNotFound)
	}
	return webhookFrom(row), nil
}

func (r *webhookRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.WebhookModel, error) {
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := webhook.New().Order(webhook.Name.Asc())
	if scoped != nil {
		q = q.Where(webhook.ProjectID.In(uuidsToRaw(scoped)...))
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list webhooks: %w", err)
	}
	out := make([]models.WebhookModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, webhookFrom(row))
	}
	return out, nil
}

func (r *webhookRepository) Create(ctx context.Context, w models.WebhookModel) error {
	projectID := uuid.UUID(w.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := webhook.Create()
	if id := uuid.UUID(w.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(projectID)
	ins.SetName(w.Name)
	ins.SetToken(w.Token)
	ins.SetSecret(w.Secret)
	ins.SetSignatureHeader(w.SignatureHeader)
	ins.SetMessageName(w.MessageName)
	ins.SetEnabled(w.Enabled)
	setOrNullString(ins.SetCorrelationExpression, ins.SetCorrelationExpressionNull, w.CorrelationExpression)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not create the webhook: %w", err)
	}
	return nil
}

// SetEnabled stops or restarts a webhook without destroying it, so an endpoint
// that is misbehaving can be silenced and the token it issued stays claimed.
func (r *webhookRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	row, err := r.one(ctx, id)
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := webhook.Mutate(row)
	mut.SetEnabled(enabled)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not change the webhook: %w", err)
	}
	return nil
}

func (r *webhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.one(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := webhook.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such webhook", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the webhook: %w", err)
	}
	return nil
}

// ClaimDelivery records a delivery id, reporting whether this caller is the
// first to claim it.
//
// The insert races on purpose. Two replicas handed the same redelivery both
// try; the unique key on (webhook, delivery_id) lets exactly one through, and
// the loser is told it is a duplicate. Checking first and inserting second
// leaves a window where both check, both find nothing, and the same event is
// processed twice.
func (r *webhookRepository) ClaimDelivery(ctx context.Context, webhookID uuid.UUID, deliveryID string) (bool, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return false, err
	}
	ins := webhookdelivery.Create()
	ins.SetWebhookID(webhookID)
	ins.SetDeliveryID(deliveryID)
	ins.SetReceivedAt(time.Now().UTC())
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return false, nil
		}
		return false, fmt.Errorf("could not record the delivery: %w", err)
	}
	return true, nil
}

// ForgetDeliveriesBefore drops the de-duplication records that are older than
// any redelivery window, so the table does not grow for ever.
func (r *webhookRepository) ForgetDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := db.DeleteInBatches(ctx, ex,
		`DELETE FROM webhook_deliveries WHERE ctid = ANY(ARRAY(
		     SELECT ctid FROM webhook_deliveries WHERE received_at < $1 LIMIT $2))`, cutoff.UTC())
	if err != nil {
		return removed, fmt.Errorf("could not forget old deliveries: %w", err)
	}
	return removed, nil
}

// one reads a webhook within the caller's scope.
func (r *webhookRepository) one(ctx context.Context, id uuid.UUID) (webhook.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return webhook.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return webhook.Row{}, err
	}
	q := webhook.New().Where(webhook.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return webhook.Row{}, fmt.Errorf("%w: no such webhook", apierr.ErrNotFound)
		}
		q = q.Where(webhook.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return webhook.Row{}, fmt.Errorf("could not read the webhook: %w", err)
	}
	if !found {
		return webhook.Row{}, fmt.Errorf("%w: no such webhook", apierr.ErrNotFound)
	}
	return row, nil
}

func webhookFrom(row webhook.Row) models.WebhookModel {
	return models.WebhookModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:             models.UUID(row.ProjectID),
		Name:                  row.Name,
		Token:                 row.Token,
		Secret:                row.Secret,
		SignatureHeader:       row.SignatureHeader,
		MessageName:           row.MessageName,
		CorrelationExpression: valueOr(row.CorrelationExpression),
		Enabled:               row.Enabled,
	}
}
