// Package webhookadmin serves the authenticated half of webhooks: registering,
// listing, switching and removing the addresses partners post to.
//
// Delivery itself is not here. It is public and served straight off the mux by
// the webhooks package, because its signature covers the raw body and must not
// pass through a decoder.
package webhookadmin

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/gobpm/server/endpoints/webhook"
	"github.com/gsoultan/gobpm/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps webhook.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/webhooks", httptransport.NewServer(
		eps.ListWebhooks, decodeList, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/webhooks", httptransport.NewServer(
		eps.CreateWebhook, decodeCreate, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/webhooks/{id}/enabled", httptransport.NewServer(
		eps.SetWebhookEnabled, decodeSetEnabled, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/webhooks/{id}", httptransport.NewServer(
		eps.DeleteWebhook, decodeDelete, common.EncodeResponse, options...))
}

func decodeList(_ context.Context, r *http.Request) (any, error) {
	return webhook.ListWebhooksRequest{ProjectID: r.URL.Query().Get("project_id")}, nil
}

func decodeCreate(_ context.Context, r *http.Request) (any, error) {
	var req webhook.CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeSetEnabled(_ context.Context, r *http.Request) (any, error) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return webhook.SetWebhookEnabledRequest{ID: r.PathValue("id"), Enabled: body.Enabled}, nil
}

func decodeDelete(_ context.Context, r *http.Request) (any, error) {
	return webhook.DeleteWebhookRequest{ID: r.PathValue("id")}, nil
}
