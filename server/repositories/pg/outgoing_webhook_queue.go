package pg

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/outgoingwebhook"
	"github.com/gsoultan/metis/server/repositories/store/outgoingwebhookdelivery"
)

type outgoingWebhookQueue struct{ conn }

// NewOutgoingWebhookQueue returns the queue of what outgoing webhooks are owed.
func NewOutgoingWebhookQueue(c *db.Conn) contracts.OutgoingWebhookQueue {
	return &outgoingWebhookQueue{conn{conn: c}}
}

// Enqueue writes one pending delivery per webhook of the project that asked
// for the event.
//
// The webhooks are read through the partial index over the live, switched-on
// rows, and their events matched here rather than in SQL: a project has a
// handful of webhooks, and every event of every project asks this question.
func (q *outgoingWebhookQueue) Enqueue(ctx context.Context, projectID uuid.UUID, event entities.OutgoingWebhookEvent, body []byte) (int, error) {
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := outgoingwebhook.New().
		Where(outgoingwebhook.ProjectID.Eq(projectID), outgoingwebhook.Enabled.Eq(true)).
		All(ctx, ex, nil)
	if err != nil {
		return 0, fmt.Errorf("could not read which webhooks want %s: %w", event, err)
	}
	var wanting []outgoingwebhook.Row
	for _, row := range rows {
		hook, err := outgoingWebhookFrom(row)
		if err != nil {
			return 0, err
		}
		if slices.Contains(hook.Events, event) {
			wanting = append(wanting, row)
		}
	}
	if len(wanting) == 0 {
		return 0, nil
	}

	sealed, err := crypto.Encrypt(string(body))
	if err != nil {
		return 0, fmt.Errorf("could not seal the %s delivery: %w", event, err)
	}
	now := nowUTC()
	for _, hook := range wanting {
		if err := insertDelivery(ctx, q, hook.ID, event, sealed, now); err != nil {
			return 0, err
		}
	}
	return len(wanting), nil
}

func insertDelivery(ctx context.Context, q *outgoingWebhookQueue, webhookID [16]byte, event entities.OutgoingWebhookEvent, sealedBody string, now time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("could not generate a delivery id: %w", err)
	}
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := outgoingwebhookdelivery.Create()
	ins.SetID(id)
	ins.SetWebhookID(webhookID)
	ins.SetEvent(string(event))
	ins.SetBody(sealedBody)
	ins.SetStatus(string(entities.DeliveryPending))
	ins.SetAttempts(0)
	ins.SetNextAttemptAt(now)
	ins.SetLastAttemptAtNull()
	ins.SetLastStatusCodeNull()
	ins.SetLastErrorNull()
	ins.SetLastResponseNull()
	ins.SetTest(false)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not queue the %s delivery: %w", event, err)
	}
	return nil
}

// claimDueSQL takes the due deliveries of switched-on webhooks and moves each
// one's next attempt past the lease, in one statement.
//
// SKIP LOCKED, so replicas claiming at once take different rows rather than
// queueing on the same ones; the update itself is the claim, so a row is
// never handed to two workers. Locking only the deliveries, not the webhooks,
// so a claim never waits on somebody switching a webhook off.
const claimDueSQL = `
	UPDATE outgoing_webhook_deliveries d
	   SET next_attempt_at = $1, attempts = d.attempts + 1, updated_at = $2
	 WHERE d.id IN (
	       SELECT q.id FROM outgoing_webhook_deliveries q
	         JOIN outgoing_webhooks w ON w.id = q.webhook_id
	        WHERE q.status = 'pending' AND q.next_attempt_at <= $2
	          AND w.enabled AND w.deleted_at IS NULL
	        ORDER BY q.next_attempt_at
	        LIMIT $3
	        FOR UPDATE OF q SKIP LOCKED)
	RETURNING d.id`

func (q *outgoingWebhookQueue) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]entities.OutgoingWebhookDelivery, error) {
	ids, err := q.claim(ctx, now, lease, limit)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := outgoingwebhookdelivery.New().
		Where(outgoingwebhookdelivery.ID.In(ids...)).
		Order(outgoingwebhookdelivery.NextAttemptAt.Asc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the claimed deliveries: %w", err)
	}
	hooks, err := q.webhooksOf(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]entities.OutgoingWebhookDelivery, 0, len(rows))
	for _, row := range rows {
		hook, live := hooks[row.WebhookID]
		if !live {
			// Claimed while there and read back gone: deleted in between.
			// What a deleted webhook was owed is never sent.
			continue
		}
		delivery, err := deliveryFrom(row, hook)
		if err != nil {
			return nil, err
		}
		out = append(out, delivery)
	}
	return out, nil
}

func (q *outgoingWebhookQueue) claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([][16]byte, error) {
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := ex.Query(ctx, claimDueSQL, []any{now.Add(lease).UTC(), now.UTC(), int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("could not claim the due deliveries: %w", err)
	}
	defer rows.Close()
	var ids [][16]byte
	for rows.Next() {
		values := rows.RawValues()
		if len(values) == 0 || len(values[0]) != len(uuid.UUID{}) {
			return nil, fmt.Errorf("could not read a claimed delivery's id")
		}
		ids = append(ids, [16]byte(values[0]))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not claim the due deliveries: %w", err)
	}
	return ids, nil
}

// webhooksOf reads the webhooks the claimed deliveries are for, with their
// secrets unsealed for signing.
func (q *outgoingWebhookQueue) webhooksOf(ctx context.Context, rows []outgoingwebhookdelivery.Row) (map[[16]byte]*entities.OutgoingWebhook, error) {
	ids := make([][16]byte, 0, len(rows))
	for _, row := range rows {
		if !slices.Contains(ids, row.WebhookID) {
			ids = append(ids, row.WebhookID)
		}
	}
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	hooks, err := outgoingwebhook.New().Where(outgoingwebhook.ID.In(ids...)).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the webhooks of the claimed deliveries: %w", err)
	}
	out := make(map[[16]byte]*entities.OutgoingWebhook, len(hooks))
	for _, row := range hooks {
		hook, err := outgoingWebhookFrom(row)
		if err != nil {
			return nil, err
		}
		if hook.Secret, err = crypto.Decrypt(row.Secret); err != nil {
			return nil, fmt.Errorf("could not unseal the secret of outgoing webhook %s: %w", hook.ID, err)
		}
		out[row.ID] = &hook
	}
	return out, nil
}

func deliveryFrom(row outgoingwebhookdelivery.Row, hook *entities.OutgoingWebhook) (entities.OutgoingWebhookDelivery, error) {
	body, err := crypto.Decrypt(row.Body)
	if err != nil {
		return entities.OutgoingWebhookDelivery{}, fmt.Errorf("could not unseal delivery %s: %w", uuid.UUID(row.ID), err)
	}
	return entities.OutgoingWebhookDelivery{
		ID:            row.ID,
		Webhook:       hook,
		Event:         entities.OutgoingWebhookEvent(row.Event),
		Body:          []byte(body),
		Status:        entities.OutgoingWebhookDeliveryStatus(row.Status),
		Attempts:      int(row.Attempts),
		NextAttemptAt: row.NextAttemptAt,
		CreatedAt:     row.CreatedAt,
	}, nil
}

// maxRecordedText is the longest error or response kept for a delivery: the
// columns'.
const maxRecordedText = 1024

func (q *outgoingWebhookQueue) Settle(ctx context.Context, delivery entities.OutgoingWebhookDelivery, attempt entities.OutgoingWebhookAttempt) error {
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	// On the claim it was made under only: a row whose attempts have moved on
	// was claimed again after this attempt's lease ran out, and the newer
	// claim is the one that decides.
	var settled outgoingwebhookdelivery.Mut
	settled.SetStatus(string(delivery.Status))
	settled.SetNextAttemptAt(delivery.NextAttemptAt.UTC())
	settled.SetLastAttemptAt(attempt.At.UTC())
	settled.SetUpdatedAt(attempt.At.UTC())
	if attempt.StatusCode != 0 {
		settled.SetLastStatusCode(int64(attempt.StatusCode))
	} else {
		settled.SetLastStatusCodeNull()
	}
	setRecordedText(attempt.Error, settled.SetLastError, settled.SetLastErrorNull)
	setRecordedText(attempt.Response, settled.SetLastResponse, settled.SetLastResponseNull)
	_, err = outgoingwebhookdelivery.New().
		Where(outgoingwebhookdelivery.ID.Eq(delivery.ID),
			outgoingwebhookdelivery.Attempts.Eq(int64(delivery.Attempts)),
			outgoingwebhookdelivery.Status.Eq(string(entities.DeliveryPending))).
		UpdateAll(ctx, ex, settled)
	if err != nil {
		return fmt.Errorf("could not record the attempt on delivery %s: %w", delivery.ID, err)
	}
	return nil
}

func (q *outgoingWebhookQueue) ForgetBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	ex, err := q.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	old := outgoingwebhookdelivery.New().Where(outgoingwebhookdelivery.CreatedAt.Lt(cutoff.UTC()))
	removed, err := db.PruneInBatches(ctx, func(limit int64) (int64, error) {
		return old.Limit(limit).DeleteBatch(ctx, ex)
	})
	if err != nil {
		return removed, fmt.Errorf("could not forget old outgoing webhook deliveries: %w", err)
	}
	return removed, nil
}

// setRecordedText writes an error or a response through set, cut to
// maxRecordedText runes, or NULL through null when there is nothing to keep.
func setRecordedText(s string, set func(string), null func()) {
	if s == "" {
		null()
		return
	}
	set(truncateRunes(s, maxRecordedText))
}

// truncateRunes cuts s to at most n runes, so a multi-byte character is never
// split into an invalid one.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
