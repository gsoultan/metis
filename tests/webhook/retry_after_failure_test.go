package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// flakyEngine fails its first sends, as a database that went away in the
// middle of a delivery would.
type flakyEngine struct {
	servicecontracts.ExecutionEngine
	failures int
}

func (e *flakyEngine) SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, variables map[string]any) error {
	if e.failures > 0 {
		e.failures--
		return errors.New("the database went away")
	}
	return e.ExecutionEngine.SendMessage(ctx, projectID, messageName, correlationKey, variables)
}

// A delivery is remembered as seen so that a sender's retry is not acted on
// twice. It was remembered before anything was done with it, so a delivery
// that then failed was remembered as delivered anyway, and the sender's retry
// was answered "duplicate" and dropped: the event was lost, and the sender had
// been told it arrived.
func TestADeliveryThatFailedIsActedOnWhenTheSenderRetries(t *testing.T) {
	h := newWebhookHarness(t)

	t.Run("the message could not be sent", func(t *testing.T) {
		service := serviceimpl.NewWebhookService(h.repo, &flakyEngine{ExecutionEngine: h.engine, failures: 1})
		hook := h.register(t, "order.paid", "")
		body := []byte(`{"order":{"id":"ORD-9"}}`)
		// Each attempt signed when it is sent, as a sender's retries are.
		send := func() (entities.WebhookOutcome, error) {
			return service.Receive(h.ctx, signedNow(hook, hook.Secret, body, "d-9"))
		}
		if _, err := send(); err == nil {
			t.Fatal("the first attempt should have failed")
		}
		if outcome, err := send(); err != nil || outcome.Duplicate {
			t.Fatalf("the retry of a delivery that failed came back %+v, %v; want it acted on", outcome, err)
		}
		if outcome, err := send(); err != nil || !outcome.Duplicate {
			t.Fatalf("a retry of the delivery that succeeded came back %+v, %v; want it recognised", outcome, err)
		}
	})

	t.Run("the delivery named nothing to correlate with", func(t *testing.T) {
		hook := h.register(t, "order.shipped", "order.id")
		for i, body := range [][]byte{[]byte(`{"order":{}}`), []byte(`{"order":{"id":"ORD-10"}}`)} {
			outcome, err := h.deliver(t, hook, body, "d-10")
			if i == 0 {
				if err == nil {
					t.Fatal("a delivery with no correlation key was accepted")
				}
				continue
			}
			if err != nil || outcome.Duplicate {
				t.Fatalf("the corrected retry came back %+v, %v; want it acted on", outcome, err)
			}
		}
	})
}
