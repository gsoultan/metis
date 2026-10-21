// Package outgoingwebhooks serves the addresses a project posts its events to:
// listing, registering, switching and removing them.
package outgoingwebhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/outgoingwebhook"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxBodyBytes bounds a request: a name, an address of at most 2,048
// characters and a few event names fit many times over.
const maxBodyBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps outgoingwebhook.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/projects/{id}/outgoing-webhooks", httptransport.NewServer(
		eps.ListOutgoingWebhooks, decodeList, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/projects/{id}/outgoing-webhooks", httptransport.NewServer(
		eps.CreateOutgoingWebhook, decodeCreate, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/outgoing-webhooks/{id}/enabled", httptransport.NewServer(
		eps.SetOutgoingWebhookEnabled, decodeSetEnabled, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/outgoing-webhooks/{id}", httptransport.NewServer(
		eps.DeleteOutgoingWebhook, decodeDelete, common.EncodeResponse, options...))
}

func decodeList(_ context.Context, r *http.Request) (any, error) {
	return outgoingwebhook.ListRequest{ProjectID: r.PathValue("id")}, nil
}

func decodeCreate(_ context.Context, r *http.Request) (any, error) {
	var req outgoingwebhook.CreateRequest
	if err := decodeStrict(r, &req, `{"name": "...", "url": "https://...", "events": ["process.completed"]}`); err != nil {
		return nil, err
	}
	req.ProjectID = r.PathValue("id")
	return req, nil
}

func decodeSetEnabled(_ context.Context, r *http.Request) (any, error) {
	var req outgoingwebhook.SetEnabledRequest
	if err := decodeStrict(r, &req, `{"enabled": true}`); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeDelete(_ context.Context, r *http.Request) (any, error) {
	return outgoingwebhook.DeleteRequest{ID: r.PathValue("id")}, nil
}

// decodeStrict reads one JSON object with no fields but dst's and nothing
// after it. shape is what the refusal tells the caller to send instead.
func decodeStrict(r *http.Request, dst any, shape string) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apierr.Invalidf("this request is larger than %d KiB, which is more than a webhook needs", maxBodyBytes>>10)
		}
		return unreadable(r, err, shape)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return unreadable(r, err, shape)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return unreadable(r, errors.New("something follows the JSON object"), shape)
	}
	return nil
}

// unreadable is the refusal of a body that could not be read, in words
// somebody can act on; why is logged, not told.
func unreadable(r *http.Request, why error, shape string) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("An outgoing webhook request could not be read")
	return apierr.Invalidf("this request could not be read: send one JSON object, %s", shape)
}
