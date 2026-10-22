// Package webhookadmin serves the authenticated half of webhooks: registering,
// listing, switching and removing the addresses partners post to, and trying a
// delivery against one without acting on it.
//
// Delivery itself is not here. It is public and served straight off the mux by
// the webhooks package, because its signature covers the raw body and must not
// pass through a decoder.
package webhookadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/webhook"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps webhook.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/webhooks", httptransport.NewServer(
		eps.ListWebhooks, decodeList, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/webhooks", httptransport.NewServer(
		eps.CreateWebhook, decodeCreate, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/webhooks/{id}/enabled", httptransport.NewServer(
		eps.SetWebhookEnabled, decodeSetEnabled, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/webhooks/{id}/legacy-signatures", httptransport.NewServer(
		eps.CloseLegacySignatures, decodeCloseLegacySignatures, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/webhooks/{id}/simulate", httptransport.NewServer(
		eps.SimulateDelivery, decodeSimulate, common.EncodeResponse, options...))
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

func decodeCloseLegacySignatures(_ context.Context, r *http.Request) (any, error) {
	return webhook.CloseLegacySignaturesRequest{ID: r.PathValue("id")}, nil
}

// maxSimulationBytes bounds a simulation request. The pasted body is held to
// the 1 MiB a real delivery is, after decoding; this leaves room for the JSON
// escaping around it and still stops a request the service would refuse
// before it is read whole.
const maxSimulationBytes = 4 << 20

func decodeSimulate(_ context.Context, r *http.Request) (any, error) {
	var req webhook.SimulateDeliveryRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxSimulationBytes)).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this simulation is larger than %d MiB, more than a delivery may be", maxSimulationBytes>>20)
		}
		return nil, apierr.Invalidf("the simulation could not be read as JSON: %v", err)
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeDelete(_ context.Context, r *http.Request) (any, error) {
	return webhook.DeleteWebhookRequest{ID: r.PathValue("id")}, nil
}
