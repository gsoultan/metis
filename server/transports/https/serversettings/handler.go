package serversettings

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
	"github.com/gsoultan/metis/server/endpoints/serversetting"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxSettingBodyBytes bounds a request to change a setting: a value of at most
// a thousand characters and a reason of at most five hundred, each at most
// four bytes.
const maxSettingBodyBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps serversetting.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/settings/server", httptransport.NewServer(
		eps.ListServerSettings,
		func(context.Context, *http.Request) (any, error) { return nil, nil },
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/settings/server/{name}", httptransport.NewServer(
		eps.ChangeServerSetting,
		decodeChangeRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/settings/server/{name}/history", httptransport.NewServer(
		eps.ServerSettingHistory,
		func(_ context.Context, r *http.Request) (any, error) {
			return serversetting.HistoryRequest{Name: r.PathValue("name")}, nil
		},
		common.EncodeResponse,
		options...,
	))
}

// changeBody is the body as sent. Value is kept raw so a body that leaves it
// out is told apart from one that sends null: leaving it out must not clear a
// setting by accident.
type changeBody struct {
	Value  json.RawMessage `json:"value"`
	Reason string          `json:"reason"`
}

// decodeChangeRequest reads {"value": "..." or null, "reason": "..."} and
// nothing else: one JSON object, no other field, nothing after it.
func decodeChangeRequest(_ context.Context, r *http.Request) (any, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxSettingBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this request is larger than %d KiB, which is more than a setting and a reason need", maxSettingBodyBytes>>10)
		}
		return nil, unreadableChange(r, err)
	}
	var sent changeBody
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sent); err != nil {
		return nil, unreadableChange(r, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, unreadableChange(r, errors.New("something follows the JSON object"))
	}
	if len(sent.Value) == 0 {
		return nil, apierr.Invalidf(`say what to set: "value" is the new value, or null to go back to the environment's or the default`)
	}
	req := serversetting.ChangeRequest{Name: r.PathValue("name"), Reason: sent.Reason}
	if err := json.Unmarshal(sent.Value, &req.Value); err != nil {
		return nil, apierr.Invalidf(`"value" is text, or null to go back to the environment's or the default`)
	}
	return req, nil
}

// unreadableChange is the refusal of a body that could not be read, in words
// somebody can act on; why is logged, not told — the body may hold a secret.
func unreadableChange(r *http.Request, why error) error {
	log.Debug().Str("path", r.URL.Path).Str("why", errorKind(why)).Msg("A request to change a server setting could not be read")
	return apierr.Invalidf(`this request could not be read: send one JSON object, {"value": "smtp.example.com", "reason": "why"}`)
}

// errorKind names what went wrong without repeating the body, which a JSON
// syntax error quotes.
func errorKind(err error) string {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return "malformed JSON"
	case errors.As(err, &typed):
		return "a field of the wrong type"
	default:
		return "unreadable body"
	}
}
