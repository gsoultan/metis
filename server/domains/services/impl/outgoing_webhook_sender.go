package impl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
)

const (
	// outgoingUserAgent names the sender. Version 2 is the signed, queued
	// sender; version 1 is WEBHOOK_ENDPOINTS.
	outgoingUserAgent = "Metis-Webhook/2.0"

	// OutgoingEventHeader names the event, so a receiver can route a delivery
	// before parsing it.
	OutgoingEventHeader = "X-Metis-Event"

	// OutgoingDeliveryIDHeader carries the delivery's id: the same on every
	// attempt, and covered by the signature. The header the inbound side reads
	// a sender's id from, so that one verifier serves both directions.
	OutgoingDeliveryIDHeader = "X-Delivery-Id"

	// maxResponseExcerpt is how much of a receiver's answer is read: enough to
	// keep the start of it for the log, and no more, whatever it sends.
	maxResponseExcerpt = 1024

	// retryJitter spreads retries by up to this fraction either way, so a
	// receiver that comes back is not met by every delivery it missed at once.
	retryJitter = 0.2
)

// outgoingWebhookSender makes one attempt at one delivery.
type outgoingWebhookSender struct {
	client *http.Client
}

// newOutgoingWebhookSender builds the sender on the guarded client, with
// redirects refused.
//
// The guarded client checks every address it dials, so a redirect could not
// reach a private one. It is refused anyway: the receiver was named by an
// administrator, and a 3xx moving the delivery and its signature somewhere
// nobody named is not something to follow quietly. It is recorded as the
// receiver's answer.
func newOutgoingWebhookSender(timeout time.Duration) *outgoingWebhookSender {
	client := httpclient.New(timeout)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &outgoingWebhookSender{client: client}
}

// post signs and posts a delivery, and says what came of it. It never
// returns an error: every way an attempt can fail is an outcome to record.
func (s *outgoingWebhookSender) post(ctx context.Context, delivery entities.OutgoingWebhookDelivery, now time.Time) entities.OutgoingWebhookAttempt {
	attempt := entities.OutgoingWebhookAttempt{At: now}
	req, err := signedRequest(ctx, delivery, now)
	if err != nil {
		attempt.Error = "the request could not be made: " + err.Error()
		attempt.Permanent = true
		return attempt
	}
	resp, err := s.client.Do(req)
	if err != nil {
		attempt.Error, attempt.Permanent = describeSendFailure(err)
		return attempt
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Msg("Could not close an outgoing webhook's response")
		}
	}()

	attempt.StatusCode = resp.StatusCode
	attempt.Response = responseExcerpt(resp.Body)
	if attempt.StatusCode < 200 || attempt.StatusCode >= 300 {
		attempt.Error = fmt.Sprintf("the receiver answered %s", resp.Status)
	}
	return attempt
}

// signedRequest builds the POST for an attempt, signed with the v2 scheme the
// inbound side verifies: the timestamp, the delivery id and the body, joined
// with dots, under HMAC-SHA256.
//
// The timestamp is the attempt's, not the event's. A receiver refuses a
// signature more than a few minutes old, and a retry is sent hours later.
func signedRequest(ctx context.Context, delivery entities.OutgoingWebhookDelivery, now time.Time) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.Webhook.URL, bytes.NewReader(delivery.Body))
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	id := delivery.ID.String()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", outgoingUserAgent)
	req.Header.Set(OutgoingEventHeader, string(delivery.Event))
	req.Header.Set(OutgoingDeliveryIDHeader, id)
	req.Header.Set(webhooksig.TimestampHeader, timestamp)
	req.Header.Set(webhooksig.SignatureHeader, webhooksig.SignV2(delivery.Body, delivery.Webhook.Secret, timestamp, id))
	return req, nil
}

// describeSendFailure says in words why no answer came, and whether trying
// again could change that.
//
// The error itself is not quoted: it can carry the resolved address of the
// receiver, which is not the reader's business, and its wording is the Go
// standard library's rather than anybody's who would read the log.
func describeSendFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, httpclient.ErrBlockedAddress):
		return "this installation does not call that address: it resolves inside a private network, or is not on the list of hosts it may call", true
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return "the receiver did not answer in time", false
	case errors.Is(err, context.Canceled):
		return "the attempt was stopped because the server is shutting down", false
	default:
		return "the receiver could not be reached", false
	}
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// responseExcerpt reads the start of an answer as text, and the rest not at
// all: the connection is not reused, which is cheaper than reading a receiver's
// gigabyte to the end.
func responseExcerpt(body io.Reader) string {
	buf, err := io.ReadAll(io.LimitReader(body, maxResponseExcerpt))
	if err != nil && len(buf) == 0 {
		return ""
	}
	text := strings.ToValidUTF8(string(buf), string(utf8.RuneError))
	return strings.TrimSpace(text)
}

// jittered spreads a wait by up to retryJitter either way.
func jittered(wait time.Duration) time.Duration {
	// #nosec G404 -- jitter, not a secret: it spreads a cohort of retries so
	// they do not land on a receiver in lockstep.
	spread := (rand.Float64()*2 - 1) * retryJitter
	return wait + time.Duration(float64(wait)*spread)
}
