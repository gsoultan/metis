package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Trying a delivery before the partner sends one.
//
// Setting a webhook up goes wrong in the same few places: the body is not what
// the correlation expression expects, the key it finds is not the one the
// process waits on, the sender signs the wrong bytes. Each of those used to be
// found by asking the partner to send again and reading a refusal. Here the
// body is pasted in and every step Receive would take is taken in order, read
// only, and reported.
//
// Nothing is written. The delivery ID is looked up, not claimed, so the real
// delivery is still the first; no message is sent, so no process moves.
//
// What is never returned is the signature the body should have carried. That
// would be a signing service for anybody allowed to simulate: paste any body,
// read back the signature, post it to the public address as the partner.

// simulatedWaitingShown is how many waiting instances are named. The count is
// always the full one; an uncorrelated message can be waited on by thousands.
const simulatedWaitingShown = 20

// SimulateWebhookDelivery runs a pasted delivery through every check Receive
// makes, and says what a real one would have done.
func (s *webhookService) SimulateWebhookDelivery(ctx context.Context, id uuid.UUID, delivery entities.WebhookSimulation) (entities.WebhookSimulationResult, error) {
	if len(delivery.Body) > maxWebhookBodyBytes {
		return entities.WebhookSimulationResult{}, apierr.Invalidf("the delivery is larger than the %d byte limit a real one is held to", maxWebhookBodyBytes)
	}
	hook, err := s.repo.Webhook().Get(ctx, id)
	if err != nil {
		return entities.WebhookSimulationResult{}, err
	}

	result := entities.WebhookSimulationResult{
		Enabled:               hook.Enabled,
		MessageName:           hook.MessageName,
		CorrelationExpression: hook.CorrelationExpression,
		Waiting:               []entities.WebhookSimulationWaiting{},
	}
	// The first refusal is the one a real delivery would be answered with, so
	// it is kept; the checks after it still run, because each says something
	// the person setting the sender up will need to fix next.
	refuse := func(reason string) {
		if result.Refusal == "" {
			result.Refusal = reason
		}
	}
	if !hook.Enabled {
		refuse("This webhook is switched off, so a real delivery would be refused whatever it carried.")
	}

	result.Signature = simulatedSignature(hook, delivery, time.Now())
	if result.Signature.Checked && !result.Signature.Valid {
		refuse(result.Signature.Problem)
	}

	payload, err := decodeWebhookPayload(delivery.Body)
	if err != nil {
		result.BodyProblem = plainly(err)
		refuse(result.BodyProblem)
		return result, nil
	}
	key, err := correlationFrom(hook.CorrelationExpression, payload)
	if err != nil {
		result.BodyProblem = plainly(err)
		refuse(result.BodyProblem)
		return result, nil
	}
	result.CorrelationKey = key

	if delivery.DeliveryID != "" {
		seen, err := s.repo.Webhook().DeliveryReceived(ctx, uuid.UUID(hook.ID), delivery.DeliveryID)
		if err != nil {
			return entities.WebhookSimulationResult{}, err
		}
		result.AlreadyReceived = seen
	}

	if err := s.simulateRecipients(ctx, hook, key, &result); err != nil {
		return entities.WebhookSimulationResult{}, err
	}

	reaches := result.WaitingTotal > 0 || len(result.Starts) > 0
	result.WouldAct = result.Refusal == "" && !result.AlreadyReceived && reaches
	return result, nil
}

// simulatedSignature checks a v2 signature given with a simulated delivery.
//
// Only v2: a sender being set up now is set up with v2, and a webhook that
// still takes legacy signatures says so on its own card. No signature is not
// a failure here. The answer says it was not checked, and the person reading
// it knows a real delivery must carry one.
func simulatedSignature(hook models.WebhookModel, delivery entities.WebhookSimulation, now time.Time) entities.WebhookSimulationSignature {
	if strings.TrimSpace(delivery.Signature) == "" {
		return entities.WebhookSimulationSignature{}
	}
	err := webhooksig.VerifyV2(delivery.Body, hook.Secret, strings.TrimSpace(delivery.Timestamp), delivery.DeliveryID, delivery.Signature, now)
	if err == nil {
		return entities.WebhookSimulationSignature{Checked: true, Valid: true}
	}
	problem := plainly(err)
	if errors.Is(err, webhooksig.ErrBadSignature) {
		problem = "The signature does not match this webhook's secret. It must be the hex HMAC-SHA256, with the secret, " +
			"of the timestamp, the delivery ID and the body exactly as sent, joined with dots, after v2=."
	}
	return entities.WebhookSimulationSignature{Checked: true, Problem: problem}
}

// simulateRecipients finds what SendMessage would reach with this key: the
// instances waiting on the message and, for an uncorrelated one, the processes
// whose start event listens for it.
func (s *webhookService) simulateRecipients(ctx context.Context, hook models.WebhookModel, key string, result *entities.WebhookSimulationResult) error {
	projectID := uuid.UUID(hook.ProjectID)
	subs, err := s.repo.Subscription().FindMessages(ctx, projectID, hook.MessageName, key)
	if err != nil {
		return err
	}
	result.WaitingTotal = len(subs)

	definitions := map[uuid.UUID]*entities.ProcessDefinition{}
	for _, sub := range subs[:min(len(subs), simulatedWaitingShown)] {
		waiting := entities.WebhookSimulationWaiting{
			InstanceID: uuid.UUID(sub.InstanceID),
			NodeID:     sub.NodeID,
			Since:      sub.CreatedAt,
		}
		instance, err := s.repo.Process().Get(ctx, waiting.InstanceID)
		if err != nil {
			// Named by its ID alone: one instance that cannot be read must not
			// hide the rest, and the count already includes it.
			result.Waiting = append(result.Waiting, waiting)
			continue
		}
		waiting.BusinessKey = instance.BusinessKey
		def, err := s.simulatedDefinition(ctx, definitions, uuid.UUID(instance.DefinitionID))
		if err == nil {
			waiting.ProcessKey = def.Key
			waiting.ProcessName = def.Name
			if node := def.FindNode(sub.NodeID); node != nil {
				waiting.NodeName = node.Name
			}
		}
		result.Waiting = append(result.Waiting, waiting)
	}

	if key != "" {
		return nil
	}
	keys, err := s.repo.Definition().ListKeysByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list the processes of the webhook's project: %w", err)
	}
	for _, processKey := range keys {
		live, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, processKey)
		if errors.Is(err, apierr.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("find the live version of %s: %w", processKey, err)
		}
		def := adapters.DefinitionEntityAdapter{Model: live}.ToEntity()
		if start := listeningStartEvent(def, "message_name", hook.MessageName); start != nil {
			result.Starts = append(result.Starts, entities.WebhookSimulationStart{
				ProcessKey:  def.Key,
				ProcessName: def.Name,
				NodeName:    start.Name,
			})
		}
	}
	return nil
}

// simulatedDefinition reads a definition once however many waiting instances
// share it.
func (s *webhookService) simulatedDefinition(ctx context.Context, cache map[uuid.UUID]*entities.ProcessDefinition, id uuid.UUID) (*entities.ProcessDefinition, error) {
	if def, ok := cache[id]; ok {
		return def, nil
	}
	m, err := s.repo.Definition().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
	cache[id] = def
	return def, nil
}

// plainly is an error as a sentence for the person reading the simulation:
// without the package prefix the log wants, and capitalised.
func plainly(err error) string {
	text := err.Error()
	for _, prefix := range []string{"webhook: ", "webhooksig: "} {
		text = strings.TrimPrefix(text, prefix)
	}
	if text == "" {
		return text
	}
	text = strings.ToUpper(text[:1]) + text[1:]
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}
