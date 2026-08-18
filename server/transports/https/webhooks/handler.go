// Package webhooks serves the public endpoint partners post events to.
//
// It is written as a plain http.Handler rather than through the go-kit
// decode/endpoint/encode pipeline the rest of the API uses, and that is
// deliberate: the signature is computed over the exact bytes delivered, so the
// body must reach the verifier unparsed and un-re-encoded. A decoder that
// unmarshalled JSON and handed on a struct would destroy the thing being
// checked — re-marshalling produces different bytes for the same document, and
// every delivery would fail.
package webhooks

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gsoultan/gobpm/internal/pkg/webhooksig"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/rs/zerolog/log"
)

// maxDeliveryBytes bounds what is read off the wire.
//
// The service checks the same limit, but this one is what stops a gigabyte
// being read into memory in the first place. The service's check covers callers
// that are not this handler.
const maxDeliveryBytes = 1 << 20 // 1 MiB

// deliveryIDHeaders are where senders put their ID for an event.
//
// Checked in order, and the first one present wins. There is no standard, so
// this is a list of what the common senders actually use — plus a neutral name
// for anyone who follows none of them.
var deliveryIDHeaders = []string{
	"X-Delivery-Id",
	"X-GitHub-Delivery",
	"X-Request-Id",
	"Idempotency-Key",
}

// RegisterRoutes mounts the public delivery endpoint.
//
// It is registered outside the authenticated API's middleware on purpose: a
// partner's webhook configuration screen has nowhere to put a bearer token this
// engine would recognise. What stands in for authentication is the signature,
// checked in the service.
func RegisterRoutes(m *http.ServeMux, svc contracts.WebhookService) {
	m.Handle("POST /api/v1/hooks/{token}", handleDelivery(svc))
}

func handleDelivery(svc contracts.WebhookService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDeliveryBytes))
		if err != nil {
			http.Error(w, `{"error":"the delivery could not be read, or is too large"}`, http.StatusRequestEntityTooLarge)
			return
		}

		// Which header carries the signature is a property of the webhook, not
		// of the request, so the webhook has to be resolved before the header
		// can be read. Rather than resolve it twice, every plausible header is
		// collected and the service picks — the signature is what it is checked
		// against, so offering a value from the wrong header cannot help an
		// attacker.
		delivery := entities.WebhookDelivery{
			Token:      token,
			Signature:  signatureFrom(r),
			DeliveryID: deliveryIDFrom(r),
			Body:       body,
		}

		outcome, err := svc.Receive(r.Context(), delivery)
		if err != nil {
			writeRejection(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, outcome)
	})
}

// signatureFrom finds the signature among the headers senders use for it.
func signatureFrom(r *http.Request) string {
	for _, header := range []string{
		entities.DefaultWebhookSignatureHeader,
		"X-Hub-Signature-256",
		"X-Signature",
		"Stripe-Signature",
		"X-Webhook-Signature",
	} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
}

func deliveryIDFrom(r *http.Request) string {
	for _, header := range deliveryIDHeaders {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
}

// writeRejection answers a delivery that was not accepted.
//
// Every rejection that concerns *who sent it* gets the same status and the same
// body. Distinguishing "no such webhook" from "wrong signature" would turn this
// endpoint into an oracle: post to a guessed address with any signature, and the
// difference in the reply tells you whether the address exists.
func writeRejection(w http.ResponseWriter, err error) {
	log.Warn().Err(err).Msg("Refused a webhook delivery")

	w.Header().Set("Content-Type", "application/json")
	switch {
	case errors.Is(err, impl.ErrUnknownWebhook),
		errors.Is(err, impl.ErrWebhookDisabled),
		isSignatureFailure(err):
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "this delivery was not accepted"})
	default:
		// Everything else is about the delivery's content — malformed JSON, a
		// correlation key that found nothing — and saying so helps whoever is
		// configuring the sending end.
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
	}
}

// isSignatureFailure reports whether err is the signature check refusing.
func isSignatureFailure(err error) bool {
	return errors.Is(err, webhooksig.ErrBadSignature) || errors.Is(err, webhooksig.ErrNoSecret)
}

// writeJSON writes a response body.
//
// A failure here means the sender hung up mid-response — there is nothing left
// to tell them and nothing to fix, so it is noted at debug and dropped. The
// alternative, ignoring it silently, hides the one case where it is not that:
// a body that cannot be encoded at all.
func writeJSON(w http.ResponseWriter, body any) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Debug().Err(err).Msg("Could not write the webhook response")
	}
}
