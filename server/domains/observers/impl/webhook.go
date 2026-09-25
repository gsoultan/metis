package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// How long an outbound webhook may take before it is abandoned. It bounds the
// whole attempt, not just the connection, so a receiver that accepts and then
// stalls cannot pin a goroutine indefinitely.
const webhookTimeout = 10 * time.Second

// WebhookObserver sends process events to external URLs.
type WebhookObserver struct {
	endpoints []string
	client    *http.Client
	// afterCommit runs a send once the transaction the event was raised in
	// has committed, and never if it rolls back.
	afterCommit func(ctx context.Context, fn func())
}

// NewWebhookObserver creates a new WebhookObserver that sends what commits.
//
// Events are dispatched inside the transaction that produced them, and this
// posted them there and then: a transaction that rolled back had still told
// the outside world the step happened. afterCommit is the unit of work's.
func NewWebhookObserver(endpoints []string, afterCommit func(ctx context.Context, fn func())) *WebhookObserver {
	return &WebhookObserver{
		endpoints:   endpoints,
		client:      &http.Client{Timeout: webhookTimeout},
		afterCommit: afterCommit,
	}
}

func (o *WebhookObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	if len(o.endpoints) == 0 {
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal webhook payload")
		return
	}

	// The webhook must not hold up — or fail — the process event that caused
	// it, so it is detached from the caller's cancellation. WithoutCancel rather
	// than Background so the trace and tenant travel with it: an outbound call
	// that cannot be correlated with the event that caused it is not much use at
	// 3am.
	detached := context.WithoutCancel(ctx)
	o.afterCommit(ctx, func() {
		for _, url := range o.endpoints {
			go o.sendWebhook(detached, url, payload)
		}
	})
}

func (o *WebhookObserver) sendWebhook(ctx context.Context, url string, payload []byte) {
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("failed to create webhook request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Metis-Webhook/1.0")

	resp, err := o.client.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("failed to send webhook")
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Str("url", url).Msg("Could not close the webhook response")
		}
	}()

	if resp.StatusCode >= 400 {
		log.Warn().Int("status", resp.StatusCode).Str("url", url).Msg("webhook returned error status")
	}
}
