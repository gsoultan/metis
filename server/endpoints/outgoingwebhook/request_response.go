package outgoingwebhook

import (
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
