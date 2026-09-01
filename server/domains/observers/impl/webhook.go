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
}

// NewWebhookObserver creates a new WebhookObserver.
func NewWebhookObserver(endpoints []string) *WebhookObserver {
	return &WebhookObserver{
		endpoints: endpoints,
		client:    &http.Client{Timeout: webhookTimeout},
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
	for _, url := range o.endpoints {
		go o.sendWebhook(detached, url, payload)
	}
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
