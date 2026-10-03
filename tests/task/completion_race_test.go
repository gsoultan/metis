package task_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// A completion and a hand-over of the same task, at the same moment.
//
// CompleteTask held the instance and then re-read the task without holding its
// row. A hand-over needs only that row, so one committed between the re-read
// and the completion's write, and the completion then wrote the row it had
// read over it: an administrator was told the task had moved, the trail said
// so, and the process advanced on the approval of the person it was taken
// from. The same window completed a task that had just been delegated.
//
// These hold one side still at a table only it reads, send the other, and let
// go — the order the bug needs, every time, rather than a race the test may
// or may not win.

// storedRefundForm is the form the step names by key. It is kept in the forms
// table, so a completion that carries the variable reads that table after it
// has re-read the task: the place a completion is held in these tests.
const storedRefundForm = "refund-form"

// answer is what a request sent from beside the test was answered with.
type answer struct {
	status int
	body   string
	err    error
}

func (a answer) String() string {
	if a.err != nil {
		return a.err.Error()
	}
	return fmt.Sprintf("%d (%s)", a.status, strings.TrimSpace(a.body))
}

// send is post for a goroutine: it reports a failure instead of stopping the
// test, which only the test's own goroutine may do.
func (h *taskHarness) send(token, path string, body any) answer {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return answer{err: err}
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+path, &buf)
	if err != nil {
		return answer{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return answer{err: err}
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return answer{status: resp.StatusCode, body: out.String()}
}

// hold takes a table for itself, so the first request to reach it waits there
// with everything it has already locked. The table is let go by the function
// returned, and at the end of the test whatever happens.
func (h *taskHarness) hold(t *testing.T, table string) (release func()) {
	t.Helper()
	tx := h.db.Begin()
	if tx.Error != nil {
		t.Fatalf("open the transaction that holds %s: %v", table, tx.Error)
	}
	if err := tx.Exec("LOCK TABLE " + table + " IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		tx.Rollback()
		t.Fatalf("hold %s: %v", table, err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			tx.Rollback()
		}
	}
	t.Cleanup(release)
	return release
}

// raceWait is how long these tests wait for a request to get where it is
// going. Far longer than it takes; it only bounds a test that has gone wrong.
const raceWait = 20 * time.Second

// heldAt waits for a request to be waiting for the table, and returns the
// database session it is waiting in.
func (h *taskHarness) heldAt(t *testing.T, table string) int {
	t.Helper()
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		var pids []int
		if err := h.db.Raw(`SELECT l.pid FROM pg_locks l
			JOIN pg_class c ON c.oid = l.relation
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = ? AND NOT l.granted`, table).Scan(&pids).Error; err != nil {
			t.Fatalf("look for the request waiting for %s: %v", table, err)
		}
		if len(pids) > 0 {
			return pids[0]
		}
	}
	t.Fatalf("no request came to wait for %s", table)
	return 0
}

// doneOrWaitingBehind waits until the request has been answered, or is waiting
// for something the session holds — and returns the answer when there is one.
func (h *taskHarness) doneOrWaitingBehind(t *testing.T, session int, answered <-chan answer) (answer, bool) {
	t.Helper()
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case got := <-answered:
			return got, true
		default:
		}
		var waiting int
		if err := h.db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))`, session).
			Scan(&waiting).Error; err != nil {
			t.Fatalf("look for a request waiting behind the held one: %v", err)
		}
		if waiting > 0 {
			return answer{}, false
		}
	}
	t.Fatal("the second request was neither answered nor made to wait")
	return answer{}, false
}

func TestAHandOverMadeWhileATaskIsBeingCompletedDoesNotLoseToTheCompletion(t *testing.T) {
	const reason = "alice must not approve this one"
	cases := []struct {
		name string
		step entities.Node
		// completes is whose completion is in flight.
		completes string
		// rival is what is sent while it is: each request in turn, stopping
		// at the first that is refused.
		rival func(h *taskHarness, boss, path string) answer
		// handedTo is who the rival gives the task to.
		handedTo string
	}{
		{
			name:      "an administrator reassigning it",
			step:      entities.Node{Name: "Approve the refund", Type: entities.UserTask, Assignee: "alice", FormKey: storedRefundForm},
			completes: "alice",
			rival: func(h *taskHarness, boss, path string) answer {
				return h.send(boss, path+"/assign", map[string]any{"user_id": "mallory", "reason": reason})
			},
			handedTo: "mallory",
		},
		{
			// Mallory may complete it while nobody holds it. Once alice has
			// claimed it and delegated it to her, it is alice's to complete.
			name: "its new holder delegating it to the person completing it",
			step: entities.Node{
				Name: "Approve the refund", Type: entities.UserTask, FormKey: storedRefundForm,
				CandidateUsers: []*entities.User{{Username: "alice"}, {Username: "mallory"}},
			},
			completes: "mallory",
			rival: func(h *taskHarness, _, path string) answer {
				if claimed := h.send(h.tokens["alice"], path+"/claim", map[string]any{}); claimed.status != http.StatusOK {
					return claimed
				}
				return h.send(h.tokens["alice"], path+"/delegate", map[string]any{"user_id": "mallory"})
			},
			handedTo: "mallory",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTaskHarness(t)
			boss := h.signInAdministrator(t, "boss")
			h.storeForm(t, storedRefundForm, map[string]any{"fields": []any{map[string]any{"id": "approved", "type": "boolean"}}})
			taskID, instanceID := h.openTaskWith(t, c.step, nil)
			path := "/api/v1/tasks/" + taskID

			release := h.hold(t, "forms")
			completion := make(chan answer, 1)
			go func() {
				completion <- h.send(h.tokens[c.completes], path+"/complete", map[string]any{"variables": map[string]any{"approved": true}})
			}()
			completing := h.heldAt(t, "forms")

			handOver := make(chan answer, 1)
			go func() { handOver <- c.rival(h, boss, path) }()
			handedOver, answered := h.doneOrWaitingBehind(t, completing, handOver)
			release()
			if !answered {
				handedOver = <-handOver
			}
			completed := <-completion
			if completed.err != nil || handedOver.err != nil {
				t.Fatalf("the completion: %v; the hand-over: %v", completed, handedOver)
			}

			status := h.taskStatus(t, taskID)
			holder, owner, state := h.delegation(t, taskID)
			row := fmt.Sprintf("the task is %s, held by %q, owner %q, delegation %q", status, holder, owner, state)

			if (completed.status == http.StatusOK) == (handedOver.status == http.StatusOK) {
				t.Fatalf("the completion was answered %v and the hand-over %v; exactly one of them is made. %s",
					completed, handedOver, row)
			}
			if completed.status == http.StatusOK {
				// The completion had the task first, so the hand-over waited
				// for it and was told the task is closed.
				if handedOver.status != http.StatusBadRequest || !strings.Contains(handedOver.body, "completed") {
					t.Errorf("the hand-over of a task completed just before it was answered %v; want a 400 saying it is completed", handedOver)
				}
				if status != string(entities.TaskCompleted) || holder != c.completes || owner != "" || state != "" {
					t.Errorf("%s; want it completed by %s and never delegated", row, c.completes)
				}
				if entries := h.trail(t, instanceID, "task_assigned"); len(entries) != 0 {
					t.Errorf("the trail has %d reassignment entries for a reassignment that was refused", len(entries))
				}
				if entries := h.trail(t, instanceID, "task_delegated"); len(entries) != 0 {
					t.Errorf("the trail has %d delegation entries for a delegation that was refused", len(entries))
				}
				return
			}
			// The hand-over was made, so the task is where it put it and open.
			if status == string(entities.TaskCompleted) || holder != c.handedTo {
				t.Errorf("%s; want it open and held by %s, who it was handed to", row, c.handedTo)
			}
		})
	}
}

// The other way round: the delegation is the one in flight, holding the task's
// row, and its holder's completion arrives. The completion read the task as it
// was before the delegation — theirs, and open — waited for the delegation at
// its own write, and then completed a task that was by then with a delegate.
func TestATaskBeingDelegatedIsNotCompletedBeforeItIsHandedBack(t *testing.T) {
	h := newTaskHarness(t)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	// A hand-over writes its entry in the trail after it has written the task.
	release := h.hold(t, "audit_logs")
	delegation := make(chan answer, 1)
	go func() {
		delegation <- h.send(h.tokens["alice"], path+"/delegate", map[string]any{"user_id": "mallory"})
	}()
	delegating := h.heldAt(t, "audit_logs")

	completion := make(chan answer, 1)
	go func() {
		completion <- h.send(h.tokens["alice"], path+"/complete", map[string]any{"variables": map[string]any{"approved": true}})
	}()
	completed, answered := h.doneOrWaitingBehind(t, delegating, completion)
	release()
	if !answered {
		completed = <-completion
	}
	delegated := <-delegation
	if completed.err != nil || delegated.err != nil {
		t.Fatalf("the delegation: %v; the completion: %v", delegated, completed)
	}

	if delegated.status != http.StatusOK {
		t.Fatalf("alice delegating her task, which had the task first: %v", delegated)
	}
	if completed.status != http.StatusForbidden || !strings.Contains(completed.body, "hand it back") {
		t.Errorf("alice completing the task she had just delegated was answered %v; want a 403 saying mallory has to hand it back", completed)
	}
	status := h.taskStatus(t, taskID)
	holder, owner, state := h.delegation(t, taskID)
	if status != string(entities.TaskDelegated) || holder != "mallory" || owner != "alice" || state != string(entities.DelegationPending) {
		t.Errorf("the task is %s, held by %q, owner %q, delegation %q; want it delegated to mallory by alice and waiting to be handed back",
			status, holder, owner, state)
	}
	if entries := h.trail(t, instanceID, "task_completed"); len(entries) != 0 {
		t.Errorf("the trail says the task was completed %d times while it was with a delegate", len(entries))
	}
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Status == entities.ProcessCompleted {
		t.Error("the process finished on the completion of a task that was with a delegate")
	}
}

// Two steps separation of duties keeps apart, open at the same time on
// parallel branches, and one person completing both at once.
//
// A completion asked whether its person had already done the other step
// before it took the instance, and never again. The other completion had by
// then written its task completed and not yet committed, so the question was
// answered from before it: both were let through, and one person had
// submitted the request and approved it.
func TestOnePersonCompletingTwoStepsKeptApartAtOnceCompletesOnlyOne(t *testing.T) {
	h := newTaskHarness(t)
	h.storeForm(t, storedRefundForm, map[string]any{"fields": []any{map[string]any{"id": "approved", "type": "boolean"}}})
	ctx := h.tenantContext()
	if _, err := h.svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "submit-and-approve",
		Name:    "Submit and approve",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "fork", Type: entities.ParallelGateway, Incoming: []string{"f1"}, Outgoing: []string{"f2", "f3"}},
			{ID: "submit", Name: "Submit the refund", Type: entities.UserTask, Assignee: "alice", FormKey: storedRefundForm,
				Incoming: []string{"f2"}, Outgoing: []string{"f4"}},
			{ID: "approve", Name: "Approve the refund", Type: entities.UserTask, Assignee: "alice",
				Properties: map[string]any{"separation_of_duties": "submit"},
				Incoming:   []string{"f3"}, Outgoing: []string{"f5"}},
			{ID: "join", Type: entities.ParallelGateway, Incoming: []string{"f4", "f5"}, Outgoing: []string{"f6"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f6"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "submit"},
			{ID: "f3", SourceRef: "fork", TargetRef: "approve"},
			{ID: "f4", SourceRef: "submit", TargetRef: "join"},
			{ID: "f5", SourceRef: "approve", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "submit-and-approve", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	page, err := h.svc.ListTasksByInstancePaged(ctx, instanceID, repocontracts.Pagination{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list the instance's tasks: %v", err)
	}
	taskOn := map[string]string{}
	for _, task := range page.Items {
		taskOn[task.NodeID()] = task.ID.String()
	}
	if taskOn["submit"] == "" || taskOn["approve"] == "" {
		t.Fatalf("the two branches opened %v; want a task on submit and one on approve", taskOn)
	}

	// The submission is held where it reads its form: it has the instance and
	// has not committed. The approval is sent then, and waits for the instance.
	release := h.hold(t, "forms")
	submission := make(chan answer, 1)
	go func() {
		submission <- h.send(h.tokens["alice"], "/api/v1/tasks/"+taskOn["submit"]+"/complete",
			map[string]any{"variables": map[string]any{"approved": true}})
	}()
	submitting := h.heldAt(t, "forms")

	approval := make(chan answer, 1)
	go func() {
		approval <- h.send(h.tokens["alice"], "/api/v1/tasks/"+taskOn["approve"]+"/complete", map[string]any{})
	}()
	approved, answered := h.doneOrWaitingBehind(t, submitting, approval)
	release()
	if !answered {
		approved = <-approval
	}
	submitted := <-submission
	if submitted.err != nil || approved.err != nil {
		t.Fatalf("the submission: %v; the approval: %v", submitted, approved)
	}

	if submitted.status != http.StatusOK {
		t.Fatalf("alice submitting, which nothing forbids her: %v", submitted)
	}
	if approved.status != http.StatusForbidden || !strings.Contains(approved.body, "may not be done by the same person") {
		t.Errorf("alice approving what she was submitting at that moment was answered %v; want the 403 separation of duties gives", approved)
	}
	completedBy := map[string]string{}
	for node, id := range taskOn {
		if h.taskStatus(t, id) == string(entities.TaskCompleted) {
			completedBy[node] = h.taskAssignee(t, id)
		}
	}
	if completedBy["submit"] != "alice" {
		t.Errorf("the submission was answered 200 and its task is completed by %q", completedBy["submit"])
	}
	if who, done := completedBy["approve"]; done {
		t.Errorf("submit and approve are both completed, by %q and %q; separation of duties keeps them apart",
			completedBy["submit"], who)
	}
}
