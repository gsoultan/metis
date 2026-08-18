package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// WebhookObserver sends process events to external URLs.
type WebhookObserver struct {
	endpoints []string
	client    *http.Client
}

// NewWebhookObserver creates a new WebhookObserver.
func NewWebhookObserver(endpoints []string) *WebhookObserver {
	return &WebhookObserver{
		endpoints: endpoints,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
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

	for _, url := range o.endpoints {
		go o.sendWebhook(url, payload)
	}
}

func (o *WebhookObserver) sendWebhook(url string, payload []byte) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		log.Error().Err(err).Str("url", url).Msg("failed to create webhook request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoBPM-Webhook/1.0")

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
