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
	return webhookFrom(row)
}

// Get reads a webhook within the caller's scope, its secret opened. A webhook
// of a project the caller cannot see is not found, as it is everywhere else.
func (r *webhookRepository) Get(ctx context.Context, id uuid.UUID) (models.WebhookModel, error) {
	row, err := r.one(ctx, id)
	if err != nil {
		return models.WebhookModel{}, err
	}
	return webhookFrom(row)
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
		m, err := webhookFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *webhookRepository) Create(ctx context.Context, w models.WebhookModel) error {
	projectID := uuid.UUID(w.ProjectID)
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	// Sealed like every other credential at rest. A database backup or a
	// read-only replica must not be enough to sign a delivery as the partner.
	secret, err := sealedText(w.Secret)
	if err != nil {
		return fmt.Errorf("could not encrypt the webhook's secret: %w", err)
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
	ins.SetSecret(secret)
	ins.SetSignatureHeader(w.SignatureHeader)
	ins.SetMessageName(w.MessageName)
	ins.SetEnabled(w.Enabled)
	setOrNullString(ins.SetCorrelationExpression, ins.SetCorrelationExpressionNull, w.CorrelationExpression)
	if until := w.LegacySignaturesUntil; until != nil {
		ins.SetLegacySignaturesUntil(until.UTC())
	} else {
		ins.SetLegacySignaturesUntilNull()
	}
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

func (r *webhookRepository) CloseLegacyWindow(ctx context.Context, id uuid.UUID, at time.Time) error {
	row, err := r.one(ctx, id)
	if err != nil {
		return err
	}
	until, open := row.LegacySignaturesUntil.Get()
	if !open || !until.After(at) {
		return nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := webhook.Mutate(row)
	mut.SetLegacySignaturesUntil(at.UTC())
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not close the webhook's window for legacy signatures: %w", err)
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
	// ON CONFLICT DO NOTHING rather than catching the unique violation. The
	// claim runs in the transaction that acts on the delivery, and on
	// PostgreSQL a statement that fails aborts its transaction: a retry that
	// found the delivery already recorded could not then commit its answer.
	ins.DoNothing()
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrConflict) || errors.Is(err, runtime.ErrUniqueViolation) {
			return false, nil
		}
		return false, fmt.Errorf("could not record the delivery: %w", err)
	}
	return true, nil
}

// DeliveryReceived looks a delivery ID up without claiming it, so asking
// leaves the real delivery free to be the first.
func (r *webhookRepository) DeliveryReceived(ctx context.Context, webhookID uuid.UUID, deliveryID string) (bool, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return false, err
	}
	found, err := webhookdelivery.New().
		Where(webhookdelivery.WebhookID.Eq(webhookID), webhookdelivery.DeliveryID.Eq(deliveryID)).
		Exists(ctx, ex)
	if err != nil {
		return false, fmt.Errorf("could not look the delivery up: %w", err)
	}
	return found, nil
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

// webhookFrom maps a row to the model, opening the sealed secret. A secret
// that is not sealed — written by a replica from before sealing, while an
// upgrade rolls out — is read as it was, and migration 42 seals it.
func webhookFrom(row webhook.Row) (models.WebhookModel, error) {
	secret, err := openedText(row.Secret)
	if err != nil {
		return models.WebhookModel{}, fmt.Errorf("could not decrypt the webhook's secret: %w", err)
	}
	m := models.WebhookModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:             models.UUID(row.ProjectID),
		Name:                  row.Name,
		Token:                 row.Token,
		Secret:                secret,
		SignatureHeader:       row.SignatureHeader,
		MessageName:           row.MessageName,
		CorrelationExpression: valueOr(row.CorrelationExpression),
		Enabled:               row.Enabled,
	}
	if until, ok := row.LegacySignaturesUntil.Get(); ok {
		m.LegacySignaturesUntil = &until
	}
	return m, nil
}
