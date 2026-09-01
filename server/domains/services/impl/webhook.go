package impl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/feel"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// The webhook receiver: the inbound half of an integration.
//
// A partner announces that something happened — a payment cleared, a ticket was
// closed — and a process waiting to hear it moves. The engine already accepted
// messages over its authenticated API and over AMQP, and neither is something a
// third party's webhook configuration screen can talk to.
//
// The endpoint this serves is public, which is what a webhook is. Everything
// that makes that safe is in Receive, and it is worth reading in order.

// maxWebhookBodyBytes bounds a delivery.
//
// The signature is computed over the whole body, so the whole body has to be
// held in memory before anything can be decided about it. That makes the limit
// the only thing between this endpoint and anyone who wants to post a gigabyte.
const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// deliveryRetention is how long a delivery ID is remembered.
//
// Long enough to cover any sender's retry schedule — most give up inside a day
// — and short enough that the table does not grow without bound on remote
// input.
const deliveryRetention = 48 * time.Hour

type webhookService struct {
	repo   repositories.Repository
	engine servicecontracts.ExecutionEngine
}

// NewWebhookService creates the receiver for inbound webhook deliveries.
func NewWebhookService(repo repositories.Repository, engine servicecontracts.ExecutionEngine) servicecontracts.WebhookService {
	return &webhookService{repo: repo, engine: engine}
}

// ErrUnknownWebhook is returned for a token that resolves to nothing.
//
// Callers must answer it the same way they answer a bad signature. A response
// that distinguished "no such webhook" from "wrong secret" would turn this
// endpoint into an oracle for guessing tokens.
var ErrUnknownWebhook = errors.New("webhook: no webhook answers to that address")

// ErrWebhookDisabled is returned for a webhook that has been switched off.
var ErrWebhookDisabled = errors.New("webhook: this webhook is switched off")

// Receive turns a delivery into a message, if everything about it checks out.
func (s *webhookService) Receive(ctx context.Context, delivery entities.WebhookDelivery) (entities.WebhookOutcome, error) {
	if len(delivery.Body) > maxWebhookBodyBytes {
		return entities.WebhookOutcome{}, fmt.Errorf("webhook: the delivery is larger than the %d byte limit", maxWebhookBodyBytes)
	}

	hook, err := s.repo.Webhook().GetByToken(ctx, delivery.Token)
	if err != nil {
		return entities.WebhookOutcome{}, ErrUnknownWebhook
	}
	if !hook.Enabled {
		return entities.WebhookOutcome{}, ErrWebhookDisabled
	}

	// The signature, before anything is parsed. Nothing in the body is looked at
	// — not even to see whether it is JSON — until it is known to have come from
	// someone holding the secret.
	if err := webhooksig.Verify(delivery.Body, hook.Secret, delivery.Signature); err != nil {
		log.Warn().
			Str("webhook", hook.Name).
			Str("token", redactToken(delivery.Token)).
			Msg("Rejected a webhook delivery whose signature did not match")
		return entities.WebhookOutcome{}, err
	}

	// The sender's own ID for this event, if it gave one. Without it there is no
	// way to tell a retry from a new event, so the delivery is acted on every
	// time — which is the behaviour of every webhook receiver that does not
	// dedup, and worth saying out loud.
	if delivery.DeliveryID != "" {
		first, err := s.repo.Webhook().ClaimDelivery(ctx, uuid.UUID(hook.ID), delivery.DeliveryID)
		if err != nil {
			return entities.WebhookOutcome{}, err
		}
		if !first {
			// Answered as success. A sender that gets an error retries, and
			// retrying is exactly what produced this.
			return entities.WebhookOutcome{Duplicate: true, MessageName: hook.MessageName}, nil
		}
	} else {
		log.Warn().
			Str("webhook", hook.Name).
			Msg("A delivery carried no id, so a retry of it cannot be recognised and will be acted on again")
	}

	payload, err := decodeWebhookPayload(delivery.Body)
	if err != nil {
		return entities.WebhookOutcome{}, err
	}

	correlationKey, err := correlationFrom(hook.CorrelationExpression, payload)
	if err != nil {
		return entities.WebhookOutcome{}, err
	}

	if err := s.engine.SendMessage(ctx, uuid.UUID(hook.ProjectID), hook.MessageName, correlationKey, payload); err != nil {
		return entities.WebhookOutcome{}, fmt.Errorf("webhook: the delivery arrived but the message could not be sent: %w", err)
	}

	return entities.WebhookOutcome{
		MessageName:    hook.MessageName,
		CorrelationKey: correlationKey,
	}, nil
}

// CreateWebhook registers an address, returning the token and the secret.
//
// Both are generated here rather than accepted from the caller: a token someone
// chose is a token someone can guess, and a secret someone chose is usually
// short. They are returned once, at creation, because the secret is encrypted
// at rest and no read path gives it back.
func (s *webhookService) CreateWebhook(ctx context.Context, hook entities.Webhook) (entities.Webhook, error) {
	if strings.TrimSpace(hook.MessageName) == "" {
		return entities.Webhook{}, errors.New("webhook: a webhook must say which message a delivery becomes")
	}
	if hook.Project == nil || hook.Project.ID == uuid.Nil {
		return entities.Webhook{}, errors.New("webhook: a webhook belongs to a project")
	}

	token, err := randomToken(24)
	if err != nil {
		return entities.Webhook{}, err
	}
	secret, err := randomToken(32)
	if err != nil {
		return entities.Webhook{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return entities.Webhook{}, fmt.Errorf("webhook: could not generate an id: %w", err)
	}

	signatureHeader := strings.TrimSpace(hook.SignatureHeader)
	if signatureHeader == "" {
		signatureHeader = entities.DefaultWebhookSignatureHeader
	}

	m := models.WebhookModel{
		Base:                  models.Base{ID: models.UUID(id)},
		ProjectID:             models.UUID(hook.Project.ID),
		Name:                  hook.Name,
		Token:                 token,
		Secret:                secret,
		SignatureHeader:       signatureHeader,
		MessageName:           hook.MessageName,
		CorrelationExpression: hook.CorrelationExpression,
		Enabled:               true,
	}
	if err := s.repo.Webhook().Create(ctx, m); err != nil {
		return entities.Webhook{}, err
	}

	hook.ID = id
	hook.Token = token
	hook.Secret = secret
	hook.SignatureHeader = signatureHeader
	hook.Enabled = true
	return hook, nil
}

func (s *webhookService) ListWebhooks(ctx context.Context, projectID uuid.UUID) ([]entities.Webhook, error) {
	list, err := s.repo.Webhook().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]entities.Webhook, len(list))
	for i, m := range list {
		out[i] = entities.Webhook{
			ID:                    uuid.UUID(m.ID),
			Project:               &entities.Project{ID: uuid.UUID(m.ProjectID)},
			Name:                  m.Name,
			Token:                 m.Token,
			SignatureHeader:       m.SignatureHeader,
			MessageName:           m.MessageName,
			CorrelationExpression: m.CorrelationExpression,
			Enabled:               m.Enabled,
		}
	}
	return out, nil
}

func (s *webhookService) SetWebhookEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	return s.repo.Webhook().SetEnabled(ctx, id, enabled)
}

func (s *webhookService) DeleteWebhook(ctx context.Context, id uuid.UUID) error {
	return s.repo.Webhook().Delete(ctx, id)
}

// ForgetOldDeliveries drops delivery records past the retention window.
func (s *webhookService) ForgetOldDeliveries(ctx context.Context) (int64, error) {
	return s.repo.Webhook().ForgetDeliveriesBefore(ctx, time.Now().Add(-deliveryRetention))
}

// decodeWebhookPayload reads the delivered body as the variables a process sees.
//
// JSON objects only. A top-level array or a bare string has no field names, so
// nothing downstream could refer to any part of it, and accepting it would mean
// inventing names for someone else's data.
func decodeWebhookPayload(body []byte) (map[string]any, error) {
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("webhook: the delivery is not a JSON object: %w", err)
	}
	return payload, nil
}

// correlationFrom picks the value that says which instance is waiting.
//
// An empty expression means the message is not correlated to one instance,
// which is how a message start event works — every delivery starts a process.
func correlationFrom(expression string, payload map[string]any) (string, error) {
	if strings.TrimSpace(expression) == "" {
		return "", nil
	}
	value, err := feel.Evaluate(expression, payload)
	if err != nil {
		return "", fmt.Errorf("webhook: could not read %q from the delivery: %w", expression, err)
	}

	// A field the delivery did not carry evaluates to null, and null renders as
	// the four characters "null" — which would be accepted as a correlation key
	// and match an instance waiting on the literal string. The kind is checked
	// rather than the text for exactly that reason.
	if value.Kind == feel.KindNull {
		return "", fmt.Errorf("webhook: %q found nothing in the delivery; the process it should reach cannot be identified", expression)
	}
	key := value.String()
	if key == "" {
		// An empty key matches every waiting instance, which is worse than
		// matching none.
		return "", fmt.Errorf("webhook: %q is empty in the delivery, and an empty key would match every waiting process", expression)
	}
	return key, nil
}

// randomToken returns a URL-safe random string of n bytes' entropy.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("webhook: could not generate a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// redactToken keeps a log line useful without putting the address in it.
func redactToken(token string) string {
	if len(token) <= 6 {
		return "…"
	}
	return token[:6] + "…"
}
