package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/outgoingwebhookdelivery"
)

// A webhook's log, and what an administrator can do to one delivery in it.
// Every read and write here goes through the webhook's scope first, so a
// delivery of another organization's webhook is answered as one that is not
// there.

const (
	// defaultLogPage and maxLogPage bound a page of the log.
	defaultLogPage = 50
	maxLogPage     = 200
)

var errNoOutgoingDelivery = fmt.Errorf("%w: no such delivery", apierr.ErrNotFound)

func (r *outgoingWebhookRepository) Signing(ctx context.Context, id uuid.UUID) (entities.OutgoingWebhook, error) {
	row, err := r.one(ctx, id)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	hook, err := outgoingWebhookFrom(row)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	if hook.Secret, err = crypto.Decrypt(row.Secret); err != nil {
		return entities.OutgoingWebhook{}, fmt.Errorf("could not unseal the secret of outgoing webhook %s: %w", hook.ID, err)
	}
	return hook, nil
}

func (r *outgoingWebhookRepository) Deliveries(ctx context.Context, webhookID uuid.UUID, filter entities.OutgoingWebhookLogFilter) ([]entities.OutgoingWebhookLogEntry, error) {
	if _, err := r.one(ctx, webhookID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// Newest first, through ix_outgoing_webhook_deliveries_webhook.
	q := outgoingwebhookdelivery.New().
		Where(outgoingwebhookdelivery.WebhookID.Eq(webhookID)).
		Order(outgoingwebhookdelivery.CreatedAt.Desc(), outgoingwebhookdelivery.ID.Desc()).
		Limit(int64(logPage(filter.Limit)))
	if filter.Status != "" {
		q = q.Where(outgoingwebhookdelivery.Status.Eq(string(filter.Status)))
	}
	if before := filter.Before; before != nil {
		q = q.After(outgoingwebhookdelivery.Row{CreatedAt: before.CreatedAt.UTC(), ID: before.ID})
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the webhook's deliveries: %w", err)
	}
	out := make([]entities.OutgoingWebhookLogEntry, 0, len(rows))
	for _, row := range rows {
		entry, err := logEntryFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func logPage(asked int) int {
	switch {
	case asked <= 0:
		return defaultLogPage
	case asked > maxLogPage:
		return maxLogPage
	default:
		return asked
	}
}

// RetryDelivery makes a delivery due now. A pending one keeps its count, so
// its schedule goes on from where it was; a failed one has run out, so the
// attempt this allows is its last unless it is accepted.
func (r *outgoingWebhookRepository) RetryDelivery(ctx context.Context, deliveryID uuid.UUID, now time.Time) error {
	row, err := r.delivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if err := refuseUnless(row, entities.DeliveryPending, entities.DeliveryFailed); err != nil {
		return err
	}
	var due outgoingwebhookdelivery.Mut
	due.SetStatus(string(entities.DeliveryPending))
	due.SetNextAttemptAt(now.UTC())
	due.SetUpdatedAt(now.UTC())
	return r.change(ctx, row, due, entities.DeliveryPending, entities.DeliveryFailed)
}

// StopDelivery fails a pending delivery. A worker holding it settles nothing
// afterwards: its settle is guarded on the row still being pending.
func (r *outgoingWebhookRepository) StopDelivery(ctx context.Context, deliveryID uuid.UUID, now time.Time, reason string) error {
	row, err := r.delivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if err := refuseUnless(row, entities.DeliveryPending); err != nil {
		return err
	}
	var stopped outgoingwebhookdelivery.Mut
	stopped.SetStatus(string(entities.DeliveryFailed))
	setRecordedText(reason, stopped.SetLastError, stopped.SetLastErrorNull)
	stopped.SetUpdatedAt(now.UTC())
	return r.change(ctx, row, stopped, entities.DeliveryPending)
}

// change runs one guarded update on a delivery that is not a test and is in
// one of from, and says so when the delivery moved on between the read and
// the write.
func (r *outgoingWebhookRepository) change(ctx context.Context, row outgoingwebhookdelivery.Row, m outgoingwebhookdelivery.Mut, from ...entities.OutgoingWebhookDeliveryStatus) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	statuses := make([]string, len(from))
	for i, status := range from {
		statuses[i] = string(status)
	}
	changed, err := outgoingwebhookdelivery.New().
		Where(outgoingwebhookdelivery.ID.Eq(row.ID),
			outgoingwebhookdelivery.Test.Eq(false),
			outgoingwebhookdelivery.Status.In(statuses...)).
		UpdateAll(ctx, ex, m)
	if err != nil {
		return fmt.Errorf("could not change delivery %s: %w", uuid.UUID(row.ID), err)
	}
	if changed == 0 {
		return apierr.Invalidf("this delivery changed while you were looking at it; read the log again")
	}
	return nil
}

func (r *outgoingWebhookRepository) RecordTest(ctx context.Context, webhookID uuid.UUID, test entities.OutgoingWebhookTest) error {
	if _, err := r.one(ctx, webhookID); err != nil {
		return err
	}
	sealed, err := crypto.Encrypt(string(test.Body))
	if err != nil {
		return fmt.Errorf("could not seal the test delivery: %w", err)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	status := entities.DeliveryFailed
	if test.Attempt.Accepted() {
		status = entities.DeliveryDelivered
	}
	at := test.Attempt.At.UTC()
	ins := outgoingwebhookdelivery.Create()
	ins.SetID(test.DeliveryID)
	ins.SetWebhookID(webhookID)
	ins.SetEvent(string(test.Event))
	ins.SetBody(sealed)
	ins.SetStatus(string(status))
	ins.SetAttempts(1)
	ins.SetNextAttemptAt(at)
	ins.SetLastAttemptAt(at)
	if test.Attempt.StatusCode != 0 {
		ins.SetLastStatusCode(int64(test.Attempt.StatusCode))
	} else {
		ins.SetLastStatusCodeNull()
	}
	setOrNullString(ins.SetLastError, ins.SetLastErrorNull, truncateRunes(test.Attempt.Error, maxRecordedText))
	setOrNullString(ins.SetLastResponse, ins.SetLastResponseNull, truncateRunes(test.Attempt.Response, maxRecordedText))
	ins.SetTest(true)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not log the test delivery: %w", err)
	}
	return nil
}

// delivery reads one delivery, within the scope of its webhook.
func (r *outgoingWebhookRepository) delivery(ctx context.Context, id uuid.UUID) (outgoingwebhookdelivery.Row, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return outgoingwebhookdelivery.Row{}, err
	}
	row, found, err := outgoingwebhookdelivery.New().Where(outgoingwebhookdelivery.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return outgoingwebhookdelivery.Row{}, fmt.Errorf("could not read the delivery: %w", err)
	}
	if !found {
		return outgoingwebhookdelivery.Row{}, errNoOutgoingDelivery
	}
	if _, err := r.one(ctx, row.WebhookID); err != nil {
		// Another organization's, or its webhook was deleted: not there.
		return outgoingwebhookdelivery.Row{}, errNoOutgoingDelivery
	}
	return row, nil
}

// refuseUnless says, in words, why a delivery standing elsewhere cannot be
// changed.
func refuseUnless(row outgoingwebhookdelivery.Row, allowed ...entities.OutgoingWebhookDeliveryStatus) error {
	if row.Test {
		return apierr.Invalidf("a test delivery is never retried; send another test instead")
	}
	status := entities.OutgoingWebhookDeliveryStatus(row.Status)
	for _, ok := range allowed {
		if status == ok {
			return nil
		}
	}
	if status == entities.DeliveryDelivered {
		return apierr.Invalidf("this delivery was accepted by the receiver; there is nothing left to do")
	}
	return apierr.Invalidf("this delivery has already failed; retry it instead")
}

func logEntryFrom(row outgoingwebhookdelivery.Row) (entities.OutgoingWebhookLogEntry, error) {
	body, err := crypto.Decrypt(row.Body)
	if err != nil {
		return entities.OutgoingWebhookLogEntry{}, fmt.Errorf("could not unseal delivery %s: %w", uuid.UUID(row.ID), err)
	}
	entry := entities.OutgoingWebhookLogEntry{
		ID:            row.ID,
		WebhookID:     row.WebhookID,
		Event:         entities.OutgoingWebhookEvent(row.Event),
		Status:        entities.OutgoingWebhookDeliveryStatus(row.Status),
		Attempts:      int(row.Attempts),
		NextAttemptAt: row.NextAttemptAt,
		Test:          row.Test,
		Body:          []byte(body),
		CreatedAt:     row.CreatedAt,
	}
	if row.LastAttemptAt.Valid {
		at := row.LastAttemptAt.V
		entry.LastAttemptAt = &at
	}
	if row.LastStatusCode.Valid {
		entry.LastStatusCode = int(row.LastStatusCode.V)
	}
	if row.LastError.Valid {
		entry.LastError = row.LastError.V
	}
	if row.LastResponse.Valid {
		entry.LastResponse = row.LastResponse.V
	}
	return entry, nil
}
