package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The outgoing half: a project's events, owed to the receivers that asked for
// them, from a queue that commits with the step that raised each one.

type outgoingHarness struct {
	db        *gorm.DB
	repo      repositories.Repository
	svc       services.ServiceFacade
	ctx       context.Context
	projectID uuid.UUID
}

func newOutgoingHarness(t *testing.T) *outgoingHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	// As the server wires it.
	dispatcher.Register(observersimpl.NewOutgoingWebhookObserver(repo.OutgoingWebhookQueue(), repo.UnitOfWork().Attempt))
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(), "outgoing-webhook-test", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "purchase",
		Name:    "Purchase approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Manager approval", Assignee: "alice"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	return &outgoingHarness{db: db, repo: repo, svc: svc, ctx: ctx, projectID: projectID}
}

func (h *outgoingHarness) register(t *testing.T, target string, events ...entities.OutgoingWebhookEvent) entities.OutgoingWebhook {
	t.Helper()
	hook, err := h.svc.CreateOutgoingWebhook(h.ctx, entities.OutgoingWebhook{
		Project: &entities.Project{ID: h.projectID},
		Name:    "Ledger",
		URL:     target,
		Events:  events,
	})
	if err != nil {
		t.Fatalf("create outgoing webhook: %v", err)
	}
	return hook
}

// claimAll takes everything due now, as the worker would.
func (h *outgoingHarness) claimAll(t *testing.T, now time.Time) []entities.OutgoingWebhookDelivery {
	t.Helper()
	due, err := h.repo.OutgoingWebhookQueue().ClaimDue(entities.WithSystemContext(h.ctx), now, time.Minute, 100)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return due
}

func (h *outgoingHarness) runCase(t *testing.T, variables map[string]any) uuid.UUID {
	t.Helper()
	instanceID, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", variables)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := h.svc.ListTasks(h.ctx, h.projectID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected the approval task: %d tasks, err=%v", len(tasks), err)
	}
	if err := h.svc.CompleteTask(h.ctx, tasks[0].ID, "alice", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	return instanceID
}

// A step that commits owes each receiver the events it asked for, and only
// those, with the case named and none of its data.
func TestACommittedStepIsOwedToTheReceiversThatAskedForIt(t *testing.T) {
	h := newOutgoingHarness(t)
	ledger := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingTaskCreated, entities.OutgoingProcessCompleted)
	h.register(t, "https://pager.example.com/hooks", entities.OutgoingProcessStarted)

	instanceID := h.runCase(t, map[string]any{"salary": 98765})

	due := h.claimAll(t, time.Now())
	byWebhook := map[uuid.UUID][]entities.OutgoingWebhookEvent{}
	for _, delivery := range due {
		byWebhook[delivery.Webhook.ID] = append(byWebhook[delivery.Webhook.ID], delivery.Event)
		if strings.Contains(string(delivery.Body), "98765") {
			t.Errorf("a delivery carries the instance's variables: %s", delivery.Body)
		}
		var payload entities.OutgoingWebhookPayload
		if err := json.Unmarshal(delivery.Body, &payload); err != nil {
			t.Fatalf("delivery body: %v", err)
		}
		if payload.Instance == nil || payload.Instance.ID != instanceID {
			t.Errorf("delivery %s names instance %+v, want %s", delivery.Event, payload.Instance, instanceID)
		}
	}
	got := byWebhook[ledger.ID]
	slices.Sort(got)
	if want := []entities.OutgoingWebhookEvent{entities.OutgoingProcessCompleted, entities.OutgoingTaskCreated}; !slices.Equal(got, want) {
		t.Errorf("the ledger is owed %v, want %v", got, want)
	}
	if len(due) != 3 {
		t.Errorf("%d deliveries are owed, want 3: two to the ledger and one to the pager", len(due))
	}
}

// A step that rolls back never happened, and nobody is told it did.
func TestAStepThatRollsBackIsOwedToNobody(t *testing.T) {
	h := newOutgoingHarness(t)
	h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted, entities.OutgoingTaskCreated)

	abandoned := errors.New("abandoned")
	err := h.repo.UnitOfWork().Do(h.ctx, func(txCtx context.Context) error {
		if _, err := h.svc.StartProcess(txCtx, h.projectID, "purchase", nil); err != nil {
			return err
		}
		return abandoned
	})
	if !errors.Is(err, abandoned) {
		t.Fatalf("the transaction ended with %v, want it abandoned", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 0 {
		t.Errorf("%d deliveries are owed for a start that rolled back", len(due))
	}
}

// The secret is what a receiver trusts a delivery by. It is shown once, and a
// copy of the table is not enough to forge a delivery.
func TestTheSigningSecretIsShownOnceAndSealedAtRest(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	if len(hook.Secret) < 32 {
		t.Fatalf("the secret returned at creation is %q, want a generated one", hook.Secret)
	}

	var stored string
	if err := h.db.Raw(`SELECT secret FROM outgoing_webhooks WHERE id = ?`, hook.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("read the stored secret: %v", err)
	}
	if stored == hook.Secret || !crypto.IsCiphertext(stored) {
		t.Errorf("the secret is stored as %q, want it sealed", stored)
	}

	listed, err := h.svc.ListOutgoingWebhooks(h.ctx, h.projectID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %d webhooks, err=%v", len(listed), err)
	}
	if listed[0].Secret != "" {
		t.Error("the list gives the secret back")
	}
}

// The same delivery is never handed to two workers, and one whose worker died
// is due again once its lease runs out.
func TestADeliveryIsClaimedByOneWorkerAtATime(t *testing.T) {
	h := newOutgoingHarness(t)
	h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	now := time.Now()
	first := h.claimAll(t, now)
	if len(first) != 1 || first[0].Attempts != 1 {
		t.Fatalf("first claim = %+v, want the one delivery on its first attempt", first)
	}
	if again := h.claimAll(t, now.Add(30*time.Second)); len(again) != 0 {
		t.Fatalf("a delivery under lease was claimed again")
	}

	retaken := h.claimAll(t, now.Add(2*time.Minute))
	if len(retaken) != 1 || retaken[0].Attempts != 2 {
		t.Fatalf("after the lease ran out the claim = %+v, want the delivery on its second attempt", retaken)
	}
	if retaken[0].ID != first[0].ID {
		t.Error("the retaken delivery has a new id; a receiver could not recognise it as the same event")
	}

	// The first worker's late answer must not overwrite the second's claim.
	stale := first[0]
	stale.Status = entities.DeliveryDelivered
	queue := h.repo.OutgoingWebhookQueue()
	if err := queue.Settle(h.ctx, stale, entities.OutgoingWebhookAttempt{StatusCode: 200, At: now}); err != nil {
		t.Fatalf("settle the stale attempt: %v", err)
	}
	var status string
	if err := h.db.Raw(`SELECT status FROM outgoing_webhook_deliveries WHERE id = ?`, first[0].ID).Scan(&status).Error; err != nil {
		t.Fatalf("read the delivery: %v", err)
	}
	if status != string(entities.DeliveryPending) {
		t.Errorf("a stale attempt settled the delivery as %s", status)
	}
}

func TestReplicasClaimingAtOnceTakeDifferentDeliveries(t *testing.T) {
	h := newOutgoingHarness(t)
	h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	const cases = 12
	for range cases {
		if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
	}

	now := time.Now()
	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			due, err := h.repo.OutgoingWebhookQueue().ClaimDue(entities.WithSystemContext(h.ctx), now, time.Minute, 5)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, delivery := range due {
				seen[delivery.ID]++
			}
		})
	}
	wg.Wait()
	for id, times := range seen {
		if times > 1 {
			t.Errorf("delivery %s was claimed %d times", id, times)
		}
	}
	if len(seen) != cases {
		t.Errorf("%d deliveries were claimed, want all %d", len(seen), cases)
	}
}

// Switching a webhook off holds what it is owed rather than losing it.
func TestASwitchedOffWebhookIsOwedButNotSent(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingTaskCreated)
	if err := h.svc.SetOutgoingWebhookEnabled(h.ctx, hook.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 0 {
		t.Fatalf("a switched-off webhook was owed %d deliveries made while it was off", len(due))
	}

	// Made while it was on, then switched off: held.
	if err := h.svc.SetOutgoingWebhookEnabled(h.ctx, hook.ID, true); err != nil {
		t.Fatalf("switch on: %v", err)
	}
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := h.svc.SetOutgoingWebhookEnabled(h.ctx, hook.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 0 {
		t.Fatalf("%d deliveries were sent to a switched-off webhook", len(due))
	}
	if err := h.svc.SetOutgoingWebhookEnabled(h.ctx, hook.ID, true); err != nil {
		t.Fatalf("switch on: %v", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 1 {
		t.Errorf("after switching back on %d deliveries are due, want the one held", len(due))
	}
}

// Another organization's webhooks are not there.
func TestAnotherOrganizationsOutgoingWebhooksAreNotThere(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingTaskCreated)

	strangerCtx, _, strangerProject := testutils.ScopedProject(t, h.repo)
	listed, err := h.svc.ListOutgoingWebhooks(strangerCtx, h.projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("another organization listed %d of this project's webhooks", len(listed))
	}
	for name, change := range map[string]func() error{
		"switch off": func() error { return h.svc.SetOutgoingWebhookEnabled(strangerCtx, hook.ID, false) },
		"delete":     func() error { return h.svc.DeleteOutgoingWebhook(strangerCtx, hook.ID) },
	} {
		if err := change(); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("another organization's %s = %v, want not found", name, err)
		}
	}
	_, err = h.svc.CreateOutgoingWebhook(strangerCtx, entities.OutgoingWebhook{
		Project: &entities.Project{ID: h.projectID},
		Name:    "Planted",
		URL:     "https://attacker.example.com/",
		Events:  []entities.OutgoingWebhookEvent{entities.OutgoingTaskCreated},
	})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization created a webhook on this project: %v", err)
	}
	_ = strangerProject
}

// End to end: the worker posts what is owed, signed so that the inbound
// verifier accepts it, and records what the receiver answered.
func TestTheWorkerPostsWhatIsOwedAndRecordsTheAnswer(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	t.Setenv("METIS_OUTGOING_WEBHOOK_POLL_INTERVAL", "50ms")

	type arrival struct {
		header http.Header
		body   []byte
	}
	arrivals := make(chan arrival, 4)
	var calls sync.Mutex
	answered := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.Lock()
		answered++
		first := answered == 1
		calls.Unlock()
		arrivals <- arrival{header: r.Header.Clone(), body: body}
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("try later"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	h := newOutgoingHarness(t)
	hook := h.register(t, receiver.URL+"/hooks", entities.OutgoingProcessStarted)
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	ctx, stop := context.WithCancel(h.ctx)
	t.Cleanup(stop)
	deliverer := serviceimpl.NewOutgoingWebhookDeliverer(h.repo.OutgoingWebhookQueue(), serviceimpl.ResolveOutgoingWebhookSettings())
	deliverer.StartOutgoingWebhookDeliveries(ctx)

	var got arrival
	select {
	case got = <-arrivals:
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was posted to the receiver")
	}
	err := webhooksig.VerifyV2(got.body, hook.Secret, got.header.Get(webhooksig.TimestampHeader),
		got.header.Get(serviceimpl.OutgoingDeliveryIDHeader), got.header.Get(webhooksig.SignatureHeader), time.Now())
	if err != nil {
		t.Errorf("the inbound verifier refused the delivery: %v", err)
	}

	row := waitForSettled(t, h, got.header.Get(serviceimpl.OutgoingDeliveryIDHeader))
	if row.Status != string(entities.DeliveryPending) || row.Attempts != 1 || row.LastStatusCode != http.StatusServiceUnavailable {
		t.Errorf("after a 503 the delivery is %+v, want pending after one attempt with the 503 recorded", row)
	}
	if row.LastResponse != "try later" {
		t.Errorf("last response = %q, want the receiver's words", row.LastResponse)
	}
	if wait := time.Until(row.NextAttemptAt); wait < 30*time.Second || wait > 2*time.Minute {
		t.Errorf("the next attempt is in %s, want about a minute", wait)
	}
}

type settledRow struct {
	Status         string
	Attempts       int
	LastStatusCode int
	LastResponse   string
	NextAttemptAt  time.Time
}

func waitForSettled(t *testing.T, h *outgoingHarness, id string) settledRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var row settledRow
		err := h.db.Raw(`SELECT status, attempts, COALESCE(last_status_code, 0) AS last_status_code,
		        COALESCE(last_response, '') AS last_response, next_attempt_at
		   FROM outgoing_webhook_deliveries WHERE id = ?`, id).Scan(&row).Error
		if err != nil {
			t.Fatalf("read the delivery: %v", err)
		}
		if row.LastStatusCode != 0 {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("the attempt on delivery %s was never recorded", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOldDeliveriesAreForgotten(t *testing.T) {
	h := newOutgoingHarness(t)
	h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	queue := h.repo.OutgoingWebhookQueue()
	if removed, err := queue.ForgetBefore(h.ctx, time.Now().Add(-time.Hour)); err != nil || removed != 0 {
		t.Fatalf("forgetting deliveries older than an hour removed %d (err=%v), want none", removed, err)
	}
	if removed, err := queue.ForgetBefore(h.ctx, time.Now().Add(time.Minute)); err != nil || removed != 1 {
		t.Fatalf("forgetting everything removed %d (err=%v), want the one delivery", removed, err)
	}
}
