package deviation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type batchStarted struct {
	Kind      string         `json:"kind"`
	Matched   int64          `json:"matched"`
	TooMany   bool           `json:"too_many"`
	Operation *batchOperated `json:"operation"`
}

type batchOperated struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
	Total       int64  `json:"total"`
	Succeeded   int64  `json:"succeeded"`
	Failed      int64  `json:"failed"`
	Skipped     int64  `json:"skipped"`
	Pending     int64  `json:"pending"`
	StoppedBy   string `json:"stopped_by"`
	StopReason  string `json:"stop_reason"`
}

type batchItems struct {
	Items []struct {
		InstanceID string `json:"instance_id"`
		Status     string `json:"status"`
		Detail     string `json:"detail"`
	} `json:"items"`
	Total int64 `json:"total"`
}

// startBatch asks for an operation on the harness's project and answers the
// reply, failing unless it is the status wanted.
func (h *deviationHarness) startBatch(t *testing.T, token string, body map[string]any, want int) batchStarted {
	t.Helper()
	status, reply := h.do(t, http.MethodPost, token, "/api/v1/projects/"+h.projID.String()+"/batch-operations", body)
	if status != want {
		t.Fatalf("asking for %v answered %d (%s), want %d", body, status, reply, want)
	}
	var started batchStarted
	if want < 300 {
		if err := json.Unmarshal([]byte(reply), &started); err != nil {
			t.Fatalf("read %s: %v", reply, err)
		}
	}
	return started
}

// batchOver waits for the worker to finish an operation and answers it.
func (h *deviationHarness) batchOver(t *testing.T, token, id string) batchOperated {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		status, reply := h.do(t, http.MethodGet, token, "/api/v1/batch-operations/"+id, nil)
		if status != http.StatusOK {
			t.Fatalf("reading the operation answered %d (%s)", status, reply)
		}
		var read struct {
			Operation batchOperated `json:"operation"`
		}
		if err := json.Unmarshal([]byte(reply), &read); err != nil {
			t.Fatalf("read %s: %v", reply, err)
		}
		if read.Operation.Status != string(entities.BatchRunning) {
			return read.Operation
		}
		if time.Now().After(deadline) {
			t.Fatalf("the operation is still running: %+v", read.Operation)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (h *deviationHarness) batchItemsOf(t *testing.T, token, id, status string) batchItems {
	t.Helper()
	path := "/api/v1/batch-operations/" + id + "/items"
	if status != "" {
		path += "?status=" + status
	}
	code, reply := h.do(t, http.MethodGet, token, path, nil)
	if code != http.StatusOK {
		t.Fatalf("reading the operation's items answered %d (%s)", code, reply)
	}
	var items batchItems
	if err := json.Unmarshal([]byte(reply), &items); err != nil {
		t.Fatalf("read %s: %v", reply, err)
	}
	return items
}

// runBatchWorker starts the worker for the rest of the test.
func (h *deviationHarness) runBatchWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.svc.StartBatchOperations(ctx)
}

func (h *deviationHarness) instanceStatus(t *testing.T, id uuid.UUID) models.ProcessStatus {
	t.Helper()
	row, err := h.repo.Process().Get(entities.WithSystemContext(context.Background()), id)
	if err != nil {
		t.Fatalf("read instance %s: %v", id, err)
	}
	return row.Status
}

// Cancelling every instance a filter matched was one request per instance.
// It is now one request: a dry run says how many the filter matches, the
// operation is worked through in the background as the administrator who
// asked, each instance is cancelled exactly as one is — its ledger row and
// all — and what came of each is kept. Only an administrator of the project's
// organization may ask, with a reason.
func TestABatchCancelEndsEveryMatchedInstanceAndKeepsWhatCameOfEach(t *testing.T) {
	h := newDeviationHarness(t)
	var instances []uuid.UUID
	for range 3 {
		instances = append(instances, h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"}))
	}
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	operator := h.signIn(t, "ops", entities.RoleOperator)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	cancel := map[string]any{"kind": "cancel", "reason": "the supplier went bust", "filter": map[string]any{"status": "active"}}

	preview := h.startBatch(t, admin, cancel, http.StatusOK)
	if preview.Matched != 3 || preview.Operation != nil {
		t.Fatalf("a request with no dry_run previewed %+v, want 3 matched and nothing started", preview)
	}
	h.startBatch(t, operator, map[string]any{"kind": "cancel", "reason": "x", "dry_run": false}, http.StatusForbidden)
	h.startBatch(t, outsider, map[string]any{"kind": "cancel", "reason": "x", "dry_run": false}, http.StatusNotFound)
	h.startBatch(t, admin, map[string]any{"kind": "cancel", "reason": " ", "dry_run": false}, http.StatusBadRequest)
	h.startBatch(t, admin, map[string]any{"kind": "delete", "reason": "x", "dry_run": false}, http.StatusBadRequest)
	h.startBatch(t, admin, map[string]any{"kind": "cancel", "reason": "x", "filter": map[string]any{"status": "asleep"}}, http.StatusBadRequest)
	h.startBatch(t, admin, map[string]any{"kind": "cancel", "reason": "x", "requested_by": "someone else"}, http.StatusBadRequest)
	for _, id := range instances {
		if got := h.instanceStatus(t, id); got != models.ProcessActive {
			t.Fatalf("a refused request changed instance %s to %s", id, got)
		}
	}

	cancel["dry_run"] = false
	started := h.startBatch(t, admin, cancel, http.StatusAccepted)
	if started.Operation == nil || started.Operation.Total != 3 || started.Operation.RequestedBy != "boss" ||
		started.Operation.Status != string(entities.BatchRunning) {
		t.Fatalf("starting the cancel answered %+v, want a running operation of 3 asked for by boss", started)
	}
	if status, reply := h.do(t, http.MethodGet, outsider, "/api/v1/batch-operations/"+started.Operation.ID, nil); status != http.StatusNotFound {
		t.Errorf("another organization reading the operation answered %d (%s), want 404", status, reply)
	}

	h.runBatchWorker(t)
	over := h.batchOver(t, operator, started.Operation.ID)
	if over.Status != string(entities.BatchCompleted) || over.Succeeded != 3 || over.Failed != 0 || over.Skipped != 0 || over.Pending != 0 {
		t.Fatalf("the cancel finished as %+v, want completed with 3 succeeded", over)
	}
	items := h.batchItemsOf(t, operator, started.Operation.ID, "succeeded")
	if items.Total != 3 || len(items.Items) != 3 {
		t.Fatalf("the succeeded items are %+v, want the 3 instances", items)
	}
	for _, id := range instances {
		if got := h.instanceStatus(t, id); got != models.ProcessCancelled {
			t.Errorf("instance %s is %s after the cancel, want cancelled", id, got)
		}
		if h.rowCount(t, id) != 1 {
			t.Errorf("instance %s has %d ledger rows, want the cancel's one", id, h.rowCount(t, id))
		}
	}

	// Nothing is left running, so nothing is matched, and nothing is started.
	h.startBatch(t, admin, cancel, http.StatusBadRequest)
	status, reply := h.do(t, http.MethodGet, operator, "/api/v1/projects/"+h.projID.String()+"/batch-operations", nil)
	var listed struct {
		Operations []batchOperated `json:"operations"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(reply), &listed) != nil || len(listed.Operations) != 1 {
		t.Errorf("the project's operations answered %d (%s), want the one cancel", status, reply)
	}
}

// openIncident raises an incident on an instance as a failed step would.
func (h *deviationHarness) openIncident(t *testing.T, instance uuid.UUID) uuid.UUID {
	t.Helper()
	row, err := h.repo.Incident().Create(entities.WithSystemContext(context.Background()), models.IncidentModel{
		Base:       models.Base{ID: models.UUID(uuid.New())},
		InstanceID: models.FromUUID(instance),
		NodeID:     "step",
		Error:      "the charge was refused",
		Status:     models.IncidentOpen,
	})
	if err != nil {
		t.Fatalf("raise an incident on %s: %v", instance, err)
	}
	return uuid.UUID(row.ID)
}

// A retry is an operator's, as resolving one incident is. Each instance's open
// incidents are resolved; one with none is skipped and says so, and is not a
// failure.
func TestABatchRetryResolvesEachInstancesIncidentsAndSkipsOneWithNone(t *testing.T) {
	h := newDeviationHarness(t)
	failing := h.startOneStep(t, entities.Node{Name: "Charge", Type: entities.UserTask, Assignee: "alice"})
	healthy := h.startOneStep(t, entities.Node{Name: "Charge", Type: entities.UserTask, Assignee: "alice"})
	first, second := h.openIncident(t, failing), h.openIncident(t, failing)
	operator := h.signIn(t, "ops", entities.RoleOperator)
	user := h.signIn(t, "reader", entities.RoleUser)

	h.startBatch(t, user, map[string]any{"kind": "retry", "reason": "x"}, http.StatusForbidden)
	attention := h.startBatch(t, operator, map[string]any{"kind": "retry", "reason": "the gateway is back", "filter": map[string]any{"needs_attention": true}}, http.StatusOK)
	if attention.Matched != 1 {
		t.Fatalf("needs_attention matched %d instances, want the one with incidents", attention.Matched)
	}

	started := h.startBatch(t, operator, map[string]any{"kind": "retry", "reason": "the gateway is back", "dry_run": false}, http.StatusAccepted)
	h.runBatchWorker(t)
	over := h.batchOver(t, operator, started.Operation.ID)
	if over.Status != string(entities.BatchCompleted) || over.Succeeded != 1 || over.Skipped != 1 || over.Failed != 0 {
		t.Fatalf("the retry finished as %+v, want one succeeded and one skipped", over)
	}
	skipped := h.batchItemsOf(t, operator, started.Operation.ID, "skipped")
	if len(skipped.Items) != 1 || skipped.Items[0].InstanceID != healthy.String() || skipped.Items[0].Detail == "" {
		t.Errorf("the skipped items are %+v, want the healthy instance with why", skipped)
	}
	incidents, err := h.repo.Incident().ListByInstance(entities.WithSystemContext(context.Background()), failing)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	for _, incident := range incidents {
		if (uuid.UUID(incident.ID) == first || uuid.UUID(incident.ID) == second) && incident.Status != models.IncidentResolved {
			t.Errorf("incident %s is %s after the retry, want resolved", uuid.UUID(incident.ID), incident.Status)
		}
	}
}

// An operation acts as the administrator who asked, as they stand at each
// turn. One whose role is taken away while it waits stops acting: the
// operation is stopped, says why, and every instance it had not reached is
// skipped and left as it was.
func TestABatchOperationStopsWhenItsRequesterMayNoLongerAct(t *testing.T) {
	h := newDeviationHarness(t)
	instance := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	started := h.startBatch(t, admin, map[string]any{"kind": "cancel", "reason": "wrong batch", "dry_run": false}, http.StatusAccepted)

	system := entities.WithSystemContext(context.Background())
	account, err := h.repo.User().GetByUsername(system, "boss")
	if err != nil {
		t.Fatalf("read boss: %v", err)
	}
	account.Roles = []string{entities.RoleUser}
	if err := h.repo.User().Update(system, account); err != nil {
		t.Fatalf("take boss's role away: %v", err)
	}
	// The worker reads accounts as a request does, and a request may see an
	// account as it was for the principal cache's few seconds.
	time.Sleep(6 * time.Second)

	reader := h.signIn(t, "ops", entities.RoleOperator)
	h.runBatchWorker(t)
	over := h.batchOver(t, reader, started.Operation.ID)
	if over.Status != string(entities.BatchStopped) || over.StoppedBy != "metis" || over.Skipped != 1 || over.Succeeded != 0 {
		t.Fatalf("the operation ended as %+v, want stopped by metis with its instance skipped", over)
	}
	if got := h.instanceStatus(t, instance); got != models.ProcessActive {
		t.Errorf("the instance is %s, want it left running", got)
	}
}

// Stopping an operation skips what it had not reached, and keeps who stopped
// it and why.
func TestStoppingABatchOperationSkipsWhatItHadNotReached(t *testing.T) {
	h := newDeviationHarness(t)
	h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	operator := h.signIn(t, "ops", entities.RoleOperator)
	started := h.startBatch(t, admin, map[string]any{"kind": "cancel", "reason": "quarter end", "dry_run": false}, http.StatusAccepted)

	stop := "/api/v1/batch-operations/" + started.Operation.ID + "/stop"
	if status, reply := h.do(t, http.MethodPost, operator, stop, map[string]string{}); status != http.StatusBadRequest {
		t.Errorf("a stop with no reason answered %d (%s), want 400", status, reply)
	}
	status, reply := h.do(t, http.MethodPost, operator, stop, map[string]string{"reason": "wrong filter"})
	var stopped struct {
		Operation batchOperated `json:"operation"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(reply), &stopped) != nil {
		t.Fatalf("stopping answered %d (%s), want 200", status, reply)
	}
	if got := stopped.Operation; got.Status != string(entities.BatchStopped) || got.Skipped != 2 || got.StoppedBy != "ops" || got.StopReason != "wrong filter" {
		t.Errorf("the stopped operation is %+v, want both instances skipped, stopped by ops for the wrong filter", got)
	}
	// An instance the worker was acting on when the stop came was acted on
	// all the same, and its record says so rather than that it was skipped.
	items := h.batchItemsOf(t, operator, started.Operation.ID, "")
	system := entities.WithSystemContext(context.Background())
	cancelled := entities.BatchItem{InstanceID: uuid.MustParse(items.Items[0].InstanceID), Status: entities.BatchItemSucceeded, Detail: "cancelled"}
	if settled, err := h.repo.BatchOperation().Settle(system, uuid.MustParse(started.Operation.ID), cancelled); err != nil || !settled {
		t.Fatalf("settling an instance acted on as the stop came answered %v, %v; want it recorded", settled, err)
	}
	skip := entities.BatchItem{InstanceID: cancelled.InstanceID, Status: entities.BatchItemSkipped, Detail: "it had already ended"}
	if settled, err := h.repo.BatchOperation().Settle(system, uuid.MustParse(started.Operation.ID), skip); err != nil || settled {
		t.Fatalf("a skip over a success answered %v, %v; want nothing replaced", settled, err)
	}
	if got := h.batchOver(t, operator, started.Operation.ID); got.Succeeded != 1 || got.Skipped != 1 || got.Pending != 0 {
		t.Errorf("after the late result the operation is %+v, want one succeeded and one skipped", got)
	}

	// A second stop answers the operation as it is.
	if status, reply := h.do(t, http.MethodPost, operator, stop, map[string]string{"reason": "again"}); status != http.StatusOK {
		t.Errorf("stopping a stopped operation answered %d (%s), want 200", status, reply)
	}
}
