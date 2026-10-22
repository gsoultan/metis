package impl

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// The registry of outgoing webhooks: where a project's events go.
//
// A webhook here is an address somebody typed, which the server will post a
// project's business events to for as long as it exists. That makes the URL
// untrusted in two ways: as a destination, it is checked against the same
// egress policy every outbound call is, and as a channel out of the project,
// only an administrator may open one (see endpoints.go).

const (
	// maxOutgoingWebhookNameLength and maxOutgoingWebhookURLLength are the
	// columns'.
	maxOutgoingWebhookNameLength = 255
	maxOutgoingWebhookURLLength  = 2048

	// outgoingSecretBytes is the entropy of a generated signing secret.
	outgoingSecretBytes = 32
)

type outgoingWebhookService struct {
	webhooks contracts.OutgoingWebhookRepository
}

// NewOutgoingWebhookService creates the registry of outgoing webhooks.
func NewOutgoingWebhookService(webhooks contracts.OutgoingWebhookRepository) servicecontracts.OutgoingWebhookService {
	return &outgoingWebhookService{webhooks: webhooks}
}

// CreateOutgoingWebhook checks an address and the events asked for, and
// registers it with a secret generated here.
//
// The secret is never accepted from the caller, for the inbound webhook's
// reason: one somebody chose is usually short.
func (s *outgoingWebhookService) CreateOutgoingWebhook(ctx context.Context, hook entities.OutgoingWebhook) (entities.OutgoingWebhook, error) {
	if hook.Project == nil || hook.Project.ID == uuid.Nil {
		return entities.OutgoingWebhook{}, apierr.Invalidf("an outgoing webhook belongs to a project")
	}
	name, err := outgoingWebhookName(hook.Name)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	target, err := OutgoingWebhookURL(hook.URL)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	events, err := outgoingWebhookEvents(hook.Events)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	secret, err := randomToken(outgoingSecretBytes)
	if err != nil {
		return entities.OutgoingWebhook{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return entities.OutgoingWebhook{}, fmt.Errorf("outgoing webhook: could not generate an id: %w", err)
	}
	return s.webhooks.Create(ctx, entities.OutgoingWebhook{
		ID:      id,
		Project: &entities.Project{ID: hook.Project.ID},
		Name:    name,
		URL:     target,
		Events:  events,
		Secret:  secret,
		Enabled: true,
	})
}

func (s *outgoingWebhookService) ListOutgoingWebhooks(ctx context.Context, projectID uuid.UUID) ([]entities.OutgoingWebhook, error) {
	return s.webhooks.ListByProject(ctx, projectID)
}

func (s *outgoingWebhookService) SetOutgoingWebhookEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	return s.webhooks.SetEnabled(ctx, id, enabled)
}

func (s *outgoingWebhookService) DeleteOutgoingWebhook(ctx context.Context, id uuid.UUID) error {
	return s.webhooks.Delete(ctx, id)
}

func outgoingWebhookName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", apierr.Invalidf("give the webhook a name, so that its deliveries can be told from another's")
	}
	if utf8.RuneCountInString(name) > maxOutgoingWebhookNameLength {
		return "", apierr.Invalidf("a webhook's name is at most %d characters", maxOutgoingWebhookNameLength)
	}
	return name, nil
}

// OutgoingWebhookURL checks an address a webhook will post to, returning it as
// it will be used.
//
// HTTPS, because a delivery carries a project's business events and its
// signature, and plain HTTP shows both to everything in between; plain HTTP
// only where the installation already allows private networks, which is a
// developer's machine. No user name or password in it, because the address is
// shown back in every list. And the host must pass the egress policy here, so
// that a refused address is said when it is typed rather than discovered at
// the first delivery. The dialler checks the resolved address again on every
// attempt; this check alone proves nothing about where a name points.
func OutgoingWebhookURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", apierr.Invalidf("say where the webhook posts to")
	}
	if len(trimmed) > maxOutgoingWebhookURLLength {
		return "", apierr.Invalidf("a webhook's address is at most %d characters", maxOutgoingWebhookURLLength)
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "", apierr.Invalidf("%q is not a web address; it should look like https://example.com/hooks/metis", trimmed)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && httpclient.PrivateNetworksAllowed():
	default:
		return "", apierr.Invalidf("a webhook posts over https, so that what it sends and its signature cannot be read on the way; %q does not", trimmed)
	}
	if u.User != nil {
		return "", apierr.Invalidf("leave the user name and password out of the address: it is shown to everyone who can see the webhook. The receiver checks the signature instead")
	}
	if u.Fragment != "" {
		return "", apierr.Invalidf("an address with a #fragment cannot be posted to; remove the part from # on")
	}
	if err := httpclient.CheckURL(u); err != nil {
		if errors.Is(err, httpclient.ErrBlockedAddress) {
			return "", apierr.Invalidf("this server does not post to %s: it is inside a private network, or not on the list of hosts this installation may call. An administrator can allow it", u.Hostname())
		}
		return "", apierr.Invalidf("%q cannot be posted to: %v", trimmed, err)
	}
	return u.String(), nil
}

// outgoingWebhookEvents checks the events asked for, without duplicates and
// in the order a form lists them.
func outgoingWebhookEvents(asked []entities.OutgoingWebhookEvent) ([]entities.OutgoingWebhookEvent, error) {
	if len(asked) == 0 {
		return nil, apierr.Invalidf("choose at least one event to send")
	}
	for _, event := range asked {
		if !event.Known() {
			return nil, apierr.Invalidf("%q is not an event a webhook can be sent; choose from %s", event, knownOutgoingEvents())
		}
	}
	var events []entities.OutgoingWebhookEvent
	for _, event := range entities.OutgoingWebhookEvents() {
		if slices.Contains(asked, event) {
			events = append(events, event)
		}
	}
	return events, nil
}

func knownOutgoingEvents() string {
	names := make([]string, 0, len(entities.OutgoingWebhookEvents()))
	for _, event := range entities.OutgoingWebhookEvents() {
		names = append(names, string(event))
	}
	return strings.Join(names, ", ")
}
