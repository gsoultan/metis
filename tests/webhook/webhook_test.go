package webhook_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/webhooksig"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// A webhook endpoint is public: a partner's configuration screen has nowhere to
// put a bearer token this engine would recognise. What stands between it and
// anyone on the internet is a signature over the exact bytes delivered.
//
// These are the properties that make that safe. Each of them is a way the
// endpoint becomes a way for a stranger to start business processes.
func TestOnlyASignedDeliveryIsAccepted(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")
	body := []byte(`{"order":{"id":"ORD-1"}}`)

	t.Run("a genuine delivery is accepted", func(t *testing.T) {
		outcome, err := h.deliver(t, hook, body, webhooksig.Sign(body, hook.Secret), "evt-1")
		if err != nil {
			t.Fatalf("a correctly signed delivery was rejected: %v", err)
		}
		if outcome.CorrelationKey != "ORD-1" {
			t.Errorf("correlation key = %q, want ORD-1 read out of the payload", outcome.CorrelationKey)
		}
	})

	t.Run("an unsigned delivery is refused", func(t *testing.T) {
		if _, err := h.deliver(t, hook, body, "", "evt-2"); err == nil {
			t.Error("a delivery with no signature was accepted")
		}
	})

	t.Run("a delivery signed with the wrong secret is refused", func(t *testing.T) {
		if _, err := h.deliver(t, hook, body, webhooksig.Sign(body, "a guess"), "evt-3"); err == nil {
			t.Error("a delivery signed with the wrong secret was accepted")
		}
	})

	t.Run("a body changed after signing is refused", func(t *testing.T) {
		signature := webhooksig.Sign(body, hook.Secret)
		tampered := []byte(`{"order":{"id":"ORD-999"}}`)
		if _, err := h.deliver(t, hook, tampered, signature, "evt-4"); err == nil {
			t.Error("a delivery whose body was changed after signing was accepted")
		}
	})
}

// An unknown token must be refused the same way a bad signature is. A response
// that distinguished them would turn the endpoint into an oracle for guessing
// addresses.
func TestAnUnknownAddressIsRefused(t *testing.T) {
	h := newWebhookHarness(t)

	_, err := h.service.Receive(t.Context(), entities.WebhookDelivery{
		Token:     "not-a-real-token",
		Signature: "anything",
		Body:      []byte(`{}`),
	})
	if err == nil {
		t.Fatal("a delivery to an address that does not exist was accepted")
	}
	if !errors.Is(err, serviceimpl.ErrUnknownWebhook) {
		t.Errorf("error = %v, want ErrUnknownWebhook", err)
	}
}

// Senders retry. A partner that does not get a 2xx in time sends the same event
// again — often for hours — and every one of those would otherwise be a message
// that moves a process a second time.
func TestARetriedDeliveryIsRecognisedAndNotActedOnTwice(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")
	body := []byte(`{"order":{"id":"ORD-1"}}`)
	signature := webhooksig.Sign(body, hook.Secret)

	first, err := h.deliver(t, hook, body, signature, "delivery-42")
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if first.Duplicate {
		t.Error("the first delivery was reported as a duplicate")
	}

	second, err := h.deliver(t, hook, body, signature, "delivery-42")
	if err != nil {
		t.Fatalf("a retry was answered with an error, which is what makes senders retry: %v", err)
	}
	if !second.Duplicate {
		t.Error("a retry of the same delivery was acted on again")
	}

	// A genuinely new event with the same body still goes through — dedup is on
	// the sender's ID, not on what was sent.
	third, err := h.deliver(t, hook, body, signature, "delivery-43")
	if err != nil {
		t.Fatalf("a new delivery was rejected: %v", err)
	}
	if third.Duplicate {
		t.Error("a new event was mistaken for a retry")
	}
}

// The correlation key says which waiting instance this is about. Getting it
// wrong is worse than failing: an empty key matches every waiting instance, and
// a missing field renders as the text "null", which would match an instance
// waiting on that literal string.
func TestACorrelationKeyThatFindsNothingIsRefused(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")

	for _, body := range []string{
		`{"order":{}}`,         // the field is absent
		`{"something":"else"}`, // the whole path is absent
		`{"order":{"id":""}}`,  // present but empty
	} {
		_, err := h.deliver(t, hook, []byte(body), webhooksig.Sign([]byte(body), hook.Secret), "evt-"+body)
		if err == nil {
			t.Errorf("a delivery with no usable correlation key was accepted: %s", body)
			continue
		}
		if strings.Contains(err.Error(), "null") {
			t.Errorf("the missing key became the text \"null\": %v", err)
		}
	}
}

// A webhook with no correlation expression is how a message start event works:
// every delivery starts a process rather than moving one.
func TestAWebhookWithNoCorrelationStartsRatherThanMoves(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.received", "")
	body := []byte(`{"anything":"at all"}`)

	outcome, err := h.deliver(t, hook, body, webhooksig.Sign(body, hook.Secret), "evt-1")
	if err != nil {
		t.Fatalf("delivery: %v", err)
	}
	if outcome.CorrelationKey != "" {
		t.Errorf("correlation key = %q, want empty", outcome.CorrelationKey)
	}
}

// The secret is returned once, at creation, and never again. A secret that can
// be read back is one that will end up in a response log.
func TestTheSecretIsGivenOnceAndNeverRead(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")

	if hook.Secret == "" {
		t.Fatal("creating a webhook did not return a secret; nothing could ever sign a delivery")
	}
	if hook.Token == "" {
		t.Fatal("creating a webhook did not return a token; nothing could ever address it")
	}
	if hook.Token == hook.Secret {
		t.Fatal("the token and the secret are the same value; the one in the URL must not be the one that authenticates")
	}

	listed, err := h.service.ListWebhooks(t.Context(), h.projectID)
	if err != nil {
		t.Fatalf("list webhooks: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d webhooks, want one", len(listed))
	}
	if listed[0].Secret != "" {
		t.Error("listing webhooks gave the secret back")
	}
}

// A body that is not a JSON object has no field names, so nothing downstream
// could refer to any part of it.
func TestANonObjectBodyIsRefused(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")

	for _, body := range []string{`[1,2,3]`, `"a string"`, `not json at all`} {
		if _, err := h.deliver(t, hook, []byte(body), webhooksig.Sign([]byte(body), hook.Secret), "evt-"+body); err == nil {
			t.Errorf("a delivery that is not a JSON object was accepted: %s", body)
		}
	}
}

// A webhook that is misbehaving has to be stoppable without deleting it, because
// deleting it loses the token and the sender has to be reconfigured.
func TestADisabledWebhookAcceptsNothing(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")
	h.disable(t, hook.ID)

	body := []byte(`{}`)
	_, err := h.deliver(t, hook, body, webhooksig.Sign(body, hook.Secret), "evt-1")
	if !errors.Is(err, serviceimpl.ErrWebhookDisabled) {
		t.Errorf("error = %v, want ErrWebhookDisabled", err)
	}
}

// harness

type webhookHarness struct {
	repo      repositories.Repository
	service   servicecontracts.WebhookService
	projectID uuid.UUID
}

func newWebhookHarness(t *testing.T) *webhookHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	repo := repositories.NewRepository(db)

	dispatcher := observersimpl.NewEventDispatcher()
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	audit := serviceimpl.NewAuditWriter(repo.Audit())
	taskSvc := serviceimpl.NewTaskService(repo, engine, audit)
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), audit)),
		serviceimpl.WithJobService(jobSvc),
	)

	org, err := serviceimpl.NewOrganizationService(repo).CreateOrganization(ctx, "Hook Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	project, err := serviceimpl.NewProjectService(repo).CreateProject(ctx, org.ID, "Hook Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &webhookHarness{
		repo:      repo,
		service:   serviceimpl.NewWebhookService(repo, engine),
		projectID: project.ID,
	}
}

func (h *webhookHarness) register(t *testing.T, messageName, correlation string) entities.Webhook {
	t.Helper()
	hook, err := h.service.CreateWebhook(t.Context(), entities.Webhook{
		Project:               &entities.Project{ID: h.projectID},
		Name:                  messageName,
		MessageName:           messageName,
		CorrelationExpression: correlation,
	})
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	return hook
}

func (h *webhookHarness) deliver(t *testing.T, hook entities.Webhook, body []byte, signature, deliveryID string) (entities.WebhookOutcome, error) {
	t.Helper()
	return h.service.Receive(t.Context(), entities.WebhookDelivery{
		Token:      hook.Token,
		Signature:  signature,
		DeliveryID: deliveryID,
		Body:       body,
	})
}

func (h *webhookHarness) disable(t *testing.T, id uuid.UUID) {
	t.Helper()
	if err := h.repo.Webhook().SetEnabled(t.Context(), id, false); err != nil {
		t.Fatalf("disable webhook: %v", err)
	}
}
