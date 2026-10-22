package outgoingwebhook

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ListRequest names the project whose outgoing webhooks are listed.
type ListRequest struct {
	ProjectID string `json:"-"`
}

// ListResponse is a project's outgoing webhooks, oldest first, without their
// secrets.
type ListResponse struct {
	Webhooks []WebhookView `json:"webhooks"`
	Err      error         `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// CreateRequest registers an address for the events named. The secret is not
// accepted from the caller.
type CreateRequest struct {
	// ProjectID is taken from the address, never from the body.
	ProjectID string   `json:"-"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
}

// CreateResponse is the only place the secret ever appears.
type CreateResponse struct {
	Webhook *WebhookView `json:"webhook,omitzero"`
	Err     error        `json:"err,omitzero"`
}

func (r CreateResponse) Failed() error { return r.Err }

// SetEnabledRequest switches one on or off.
type SetEnabledRequest struct {
	ID      string `json:"-"`
	Enabled bool   `json:"enabled"`
}

// SetEnabledResponse reports the outcome.
type SetEnabledResponse struct {
	Err error `json:"err,omitzero"`
}

func (r SetEnabledResponse) Failed() error { return r.Err }

// DeleteRequest removes one.
type DeleteRequest struct {
	ID string `json:"-"`
}

// DeleteResponse reports the outcome.
type DeleteResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteResponse) Failed() error { return r.Err }

// WebhookView is an outgoing webhook as the API shows it.
type WebhookView struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`

	// Secret is set in the response that created the webhook and in no other.
	Secret string `json:"secret,omitzero"`
}

// ViewOf maps a webhook to its view, its secret included if it carries one.
func ViewOf(hook entities.OutgoingWebhook) WebhookView {
	view := WebhookView{
		ID:        hook.ID.String(),
		Name:      hook.Name,
		URL:       hook.URL,
		Events:    make([]string, 0, len(hook.Events)),
		Enabled:   hook.Enabled,
		CreatedAt: hook.CreatedAt,
		Secret:    hook.Secret,
	}
	if hook.Project != nil {
		view.ProjectID = hook.Project.ID.String()
	}
	for _, event := range hook.Events {
		view.Events = append(view.Events, string(event))
	}
	return view
}

// SendTestRequest posts a sample event to a webhook now. Event is optional;
// without it the webhook's first event is sent.
type SendTestRequest struct {
	ID    string `json:"-"`
	Event string `json:"event,omitzero"`
}

// SendTestResponse is the request exactly as posted and what came back.
type SendTestResponse struct {
	Test *TestView `json:"test,omitzero"`
	Err  error     `json:"err,omitzero"`
}

func (r SendTestResponse) Failed() error { return r.Err }

// TestView is a test delivery as the webhook's page shows it.
type TestView struct {
	DeliveryID string `json:"delivery_id"`
	Event      string `json:"event"`
	// Accepted says whether the receiver answered 2xx in time.
	Accepted   bool         `json:"accepted"`
	DurationMS int64        `json:"duration_ms"`
	Request    TestRequest  `json:"request"`
	Response   TestResponse `json:"response"`
}

// TestRequest is what was posted. Its headers carry the signature, which is
// computed from the secret and does not reveal it.
type TestRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// TestResponse is the receiver's answer: its status, the start of its body,
// or why there was none.
type TestResponse struct {
	StatusCode int    `json:"status_code,omitzero"`
	Body       string `json:"body,omitzero"`
	Error      string `json:"error,omitzero"`
}

// TestViewOf maps the outcome of a test to its view.
func TestViewOf(test entities.OutgoingWebhookTest) TestView {
	return TestView{
		DeliveryID: test.DeliveryID.String(),
		Event:      string(test.Event),
		Accepted:   test.Attempt.Accepted(),
		DurationMS: test.Duration.Milliseconds(),
		Request: TestRequest{
			Method:  http.MethodPost,
			URL:     test.URL,
			Headers: test.Headers,
			Body:    json.RawMessage(test.Body),
		},
		Response: TestResponse{
			StatusCode: test.Attempt.StatusCode,
			Body:       test.Attempt.Response,
			Error:      test.Attempt.Error,
		},
	}
}

// ListDeliveriesRequest reads a page of a webhook's log, newest first.
type ListDeliveriesRequest struct {
	ID string `json:"-"`
	// Status keeps only pending, delivered or failed deliveries.
	Status string `json:"-"`
	// Before is the next_before of the previous page.
	Before string `json:"-"`
	Limit  int    `json:"-"`
}

// ListDeliveriesResponse is a page of the log. NextBefore is set when there
// may be more, and is passed back as before to read them.
type ListDeliveriesResponse struct {
	Deliveries []DeliveryView `json:"deliveries"`
	NextBefore string         `json:"next_before,omitzero"`
	Err        error          `json:"err,omitzero"`
}

func (r ListDeliveriesResponse) Failed() error { return r.Err }

// DeliveryView is one delivery in a webhook's log.
type DeliveryView struct {
	ID             string          `json:"id"`
	WebhookID      string          `json:"webhook_id"`
	Event          string          `json:"event"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	NextAttemptAt  *time.Time      `json:"next_attempt_at,omitzero"`
	LastAttemptAt  *time.Time      `json:"last_attempt_at,omitzero"`
	LastStatusCode int             `json:"last_status_code,omitzero"`
	LastError      string          `json:"last_error,omitzero"`
	LastResponse   string          `json:"last_response,omitzero"`
	Test           bool            `json:"test,omitzero"`
	Body           json.RawMessage `json:"body"`
	CreatedAt      time.Time       `json:"created_at"`
}

// DeliveryViewOf maps a log entry to its view. When the next attempt is due
// is said only of a delivery still waiting for one.
func DeliveryViewOf(entry entities.OutgoingWebhookLogEntry) DeliveryView {
	view := DeliveryView{
		ID:             entry.ID.String(),
		WebhookID:      entry.WebhookID.String(),
		Event:          string(entry.Event),
		Status:         string(entry.Status),
		Attempts:       entry.Attempts,
		LastAttemptAt:  entry.LastAttemptAt,
		LastStatusCode: entry.LastStatusCode,
		LastError:      entry.LastError,
		LastResponse:   entry.LastResponse,
		Test:           entry.Test,
		Body:           json.RawMessage(entry.Body),
		CreatedAt:      entry.CreatedAt,
	}
	if entry.Status == entities.DeliveryPending {
		next := entry.NextAttemptAt
		view.NextAttemptAt = &next
	}
	return view
}

// DeliveryRequest names one delivery to retry or stop.
type DeliveryRequest struct {
	DeliveryID string `json:"-"`
}

// DeliveryResponse reports the outcome.
type DeliveryResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeliveryResponse) Failed() error { return r.Err }
