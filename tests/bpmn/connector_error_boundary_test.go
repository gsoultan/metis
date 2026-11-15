package bpmn_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// What a connector manifest says about its own failures.
//
// A manifest names a failure — "a 402 is CARD_DECLINED, and trying again will
// not help" — so the diagram can draw a path for it. Both halves were computed
// and then dropped: a boundary waiting for CARD_DECLINED never fired, and the
// job spent its three attempts on an answer the partner had called final.

// paymentsPartner is a partner that answers every charge with one status.
type paymentsPartner struct {
	calls atomic.Int32
	url   string
}

func newPaymentsPartner(t *testing.T, status int, header http.Header) *paymentsPartner {
	t.Helper()
	p := &paymentsPartner{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.calls.Add(1)
		for name, values := range header {
			w.Header()[name] = values
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"no"}`))
	}))
	t.Cleanup(api.Close)
	p.url = api.URL
	return p
}

// chargeManifest names a 402 as a final CARD_DECLINED and a 429 as a
// RATE_LIMITED worth waiting out.
func chargeManifest(url string) []byte {
	return []byte(`
key: pay.charge
version: 1
name: Charge a card
request:
  method: POST
  url: "` + url + `/charges"
errors:
  - when: "status = 402"
    bpmn_error: CARD_DECLINED
    retryable: false
  - when: "status = 429"
    bpmn_error: RATE_LIMITED
    retryable: true
    retry_after: "headers['Retry-After']"
`)
}

// connectPayments installs the manifest and gives the harness's project a
// connection named Payments that uses it.
func (h *serviceTaskHarness) connectPayments(t *testing.T, url string) {
	t.Helper()
	ctx := entities.WithSystemContext(h.ctx)
	if _, err := h.connectors.InstallManifest(ctx, chargeManifest(url)); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	catalogue, err := h.connectors.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("list connectors: %v", err)
	}
	for _, c := range catalogue {
		if c.Key != "pay.charge" {
			continue
		}
		_, err := h.connectors.CreateConnectorInstance(h.ctx, entities.ConnectorInstance{
			Project:   &entities.Project{ID: h.projectID},
			Connector: &entities.Connector{ID: c.ID},
			Name:      "Payments",
		})
		if err != nil {
			t.Fatalf("create connection: %v", err)
		}
		return
	}
	t.Fatal("the installed manifest is not in the catalogue")
}

// cardChargeProcess charges a card, with a boundary for boundaryCode when it is
// not empty.
func cardChargeProcess(key, boundaryCode string) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Key:  key,
		Name: "Charge the card",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card",
				Properties: map[string]any{entities.ConnectionProperty: "Payments"}},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}
	if boundaryCode == "" {
		return def
	}
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "declined", Type: entities.BoundaryEvent, Name: "If the card is declined",
			AttachedToRef: "charge", CancelActivity: true, ErrorCode: boundaryCode},
		&entities.Node{ID: "another-card", Type: entities.UserTask, Name: "Ask for another card", Assignee: "carol"},
	)
	def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "f3", SourceRef: "declined", TargetRef: "another-card"})
	return def
}

func TestAManifestFailureIsCaughtByTheBoundaryThatNamesIt(t *testing.T) {
	h := newServiceTaskHarness(t)
	partner := newPaymentsPartner(t, http.StatusPaymentRequired, nil)
	h.connectPayments(t, partner.url)

	instance := h.runDefinition(t, cardChargeProcess("declined-path", "CARD_DECLINED"), map[string]any{})

	tasks := h.tasksFor(t, instance.ID)
	if len(tasks) != 1 || tasks[0].Name != "Ask for another card" {
		t.Fatalf("got tasks %v, want the CARD_DECLINED path's task", taskNames(tasks))
	}
	if got := partner.calls.Load(); got != 1 {
		t.Errorf("the partner was called %d times, want once", got)
	}
}

func TestAManifestFailureLeavesABoundaryForAnotherCode(t *testing.T) {
	h := newServiceTaskHarness(t)
	partner := newPaymentsPartner(t, http.StatusPaymentRequired, nil)
	h.connectPayments(t, partner.url)

	instance := h.runDefinition(t, cardChargeProcess("other-code", "INSUFFICIENT_FUNDS"), map[string]any{})

	if tasks := h.tasksFor(t, instance.ID); len(tasks) != 0 {
		t.Errorf("a boundary for INSUFFICIENT_FUNDS caught CARD_DECLINED and created %v", taskNames(tasks))
	}
}

func TestAFailureTheManifestCallsFinalIsNotRetried(t *testing.T) {
	h := newServiceTaskHarness(t)
	partner := newPaymentsPartner(t, http.StatusPaymentRequired, nil)
	h.connectPayments(t, partner.url)

	instance := h.runDefinition(t, cardChargeProcess("final-failure", ""), map[string]any{})

	job := h.lastJob(t, instance.ID)
	if job.Status != entities.JobFailed {
		t.Errorf("job status = %s, want failed after the one attempt the partner called final", job.Status)
	}
	if got := partner.calls.Load(); got != 1 {
		t.Errorf("the partner was called %d times, want once", got)
	}
}

func TestARetryableFailureWaitsAsLongAsThePartnerAsked(t *testing.T) {
	h := newServiceTaskHarness(t)
	const askedSeconds = 600
	partner := newPaymentsPartner(t, http.StatusTooManyRequests, http.Header{"Retry-After": {"600"}})
	h.connectPayments(t, partner.url)

	before := time.Now()
	instance := h.runDefinition(t, cardChargeProcess("rate-limited", ""), map[string]any{})

	job := h.lastJob(t, instance.ID)
	if job.Status != entities.JobPending {
		t.Fatalf("job status = %s, want pending a retry", job.Status)
	}
	if earliest := before.Add(askedSeconds * time.Second); job.NextRunAt.Before(earliest) {
		t.Errorf("next attempt at %s, sooner than the %ds the partner asked for", job.NextRunAt.Format(time.RFC3339), askedSeconds)
	}
}
