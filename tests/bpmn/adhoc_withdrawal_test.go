package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// BPMN 2.0.2 §10.3.5 (Ad-Hoc Sub-Process, cancelRemainingInstances, default
// true) and §13.2.5: when the completionCondition becomes true, the instances
// still running inside are cancelled and the sub-process completes.
//
// The engine moved on and left them: the other step's task stayed in
// somebody's inbox, its token stayed on the instance, and an instance holding
// a token nothing will move can never end.
func TestAnAdHocSubProcessWithdrawsWhatIsStillRunningWhenItsConditionIsMet(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Withdrawal Project")
	ctx := h.Ctx()
	watcher := &withdrawals{}
	h.dispatcher.Register(watcher)

	def := adHocDefinition("claim-research-cancels", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-cancels", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "check-records"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	checks := openIterationTasks(ctx, t, h, instanceID, "check-records")
	if len(checks) != 1 {
		t.Fatalf("the records check opened %d task(s), want 1", len(checks))
	}
	checking := checks[0]
	if err := h.svc.ClaimTask(testutils.AsOperator(ctx, "dita"), checking.ID, "dita"); err != nil {
		t.Fatalf("claim the records check: %v", err)
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}
	if open := h.openTasksOn(t, instanceID, "check-records"); open != 0 {
		t.Fatalf("%d task(s) of the finished sub-process are still in somebody's inbox", open)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "check-records"); len(left) != 0 {
		t.Fatalf("the finished sub-process left %d token(s) on a step inside it", len(left))
	}
	if len(watcher.events) != 1 || watcher.events[0].Assignee != "dita" ||
		watcher.events[0].Node == nil || watcher.events[0].Node.ID != "check-records" {
		t.Fatalf("dita was not told her task went: %+v", watcher.events)
	}

	withdrawn, err := h.svc.GetTask(ctx, checking.ID)
	if err != nil {
		t.Fatalf("re-read the records check: %v", err)
	}
	requireRefusedInPlainWords(t, completeAs(ctx, h, withdrawn, "dita", nil), withdrawn)

	completeTaskAt(ctx, t, h, instanceID, "decide", nil)
	instance := requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessCompleted)
	if len(instance.Tokens) != 0 {
		t.Fatalf("the finished instance still holds %d token(s)", len(instance.Tokens))
	}
}

// BPMN 2.0.2 §10.3.5: cancelRemainingInstances set to false — the instances
// still running are not cancelled, and the sub-process completes once they
// have.
func TestAnAdHocSubProcessToldToKeepItsStepsWaitsForThem(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Keep Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-keeps", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	def.Nodes[1].Properties = map[string]any{entities.CancelRemainingInstancesProperty: false}
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-keeps", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "check-records"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the sub-process finished over a step it was told to wait for")
	}
	if open := h.openTasksOn(t, instanceID, "check-records"); open != 1 {
		t.Fatalf("the step still running has %d open task(s), want it left alone", open)
	}

	completeTaskAt(ctx, t, h, instanceID, "check-records", nil)

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the last running step finished and the sub-process still did not")
	}
	if seen := tasksEverOn(t, h, instanceID, "decide"); seen != 1 {
		t.Fatalf("the process moved past the sub-process %d times, want once", seen)
	}
}

// BPMN 2.0.2 §10.3.5, §13.2.5: the instances still running inside are
// cancelled — the ones waiting on an outside worker as much as the ones in
// somebody's inbox.
//
// Work parked for a worker is a row of its own. Left behind, a worker went on
// being offered it for a sub-process that had ended, and every report of it was
// refused, for as long as the instance existed.
func TestAnAdHocSubProcessWithdrawsTheWorkParkedForAWorkerWhenItsConditionIsMet(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Parked Work Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-parked", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	research := def.Nodes[1]
	research.Nodes = append(research.Nodes, &entities.Node{
		ID: "score-claim", Type: entities.ServiceTask, Name: "Score the claim",
		ExternalTopic: "claim-scoring", ParentID: "research",
	})
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-parked", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "score-claim"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	if parked := parkedFor(t, h, instanceID, "score-claim"); parked != 1 {
		t.Fatalf("one scoring was asked for and %d are parked", parked)
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}
	if parked := parkedFor(t, h, instanceID, "score-claim"); parked != 0 {
		t.Fatalf("%d scoring(s) are still parked for a sub-process that ended", parked)
	}
	offered, err := h.svc.FetchAndLock(ctx, "claim-scoring", "worker", 10, 60_000)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(offered) != 0 {
		t.Fatalf("a worker is still offered %d scoring(s) for a sub-process that ended", len(offered))
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "score-claim"); len(left) != 0 {
		t.Fatalf("the finished sub-process left %d token(s) on a step inside it", len(left))
	}

	trail, err := h.engine.GetAuditLogs(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	lines := 0
	for _, entry := range trail {
		if entry.Type == "parked_work_withdrawn" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("the trail has %d line(s) saying parked work was withdrawn, want 1 — for the one step that had any", lines)
	}

	completeTaskAt(ctx, t, h, instanceID, "decide", nil)
	requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessCompleted)
}

// waitingEventsOf lists the nodes of an instance that still have an event
// subscription, in the database.
func waitingEventsOf(t *testing.T, h engineHarness, instanceID uuid.UUID) []string {
	t.Helper()
	var nodes []string
	if err := h.db.Raw(`SELECT node_id FROM event_subscriptions
		 WHERE instance_id = ? AND deleted_at IS NULL ORDER BY node_id`,
		instanceID).Scan(&nodes).Error; err != nil {
		t.Fatalf("list event subscriptions: %v", err)
	}
	return nodes
}

// BPMN 2.0.2 §10.3.5, §13.2.5: a sub-process that completes cancels what is
// still running inside it, and a cancelled activity's boundary events go with
// it (§13.4.3) — the step that finished last included.
//
// A subscription that outlives its step is a message that can still arrive for
// it: delivered later, it moved a process that had left the sub-process back
// into it.
func TestAnAdHocSubProcessThatFinishesStopsWaitingForItsStepsEvents(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Waiting Events Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-events", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	research := def.Nodes[1]
	research.Nodes = append(research.Nodes,
		&entities.Node{ID: "customer-withdrew", Type: entities.BoundaryEvent, AttachedToRef: "call-customer",
			ParentID: "research", Properties: map[string]any{"message_name": "claim-withdrawn"}},
		&entities.Node{ID: "await-documents", Type: entities.IntermediateCatchEvent, Name: "Wait for the documents",
			ParentID: "research", Properties: map[string]any{"message_name": "documents-received"}},
	)
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-events", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "await-documents"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	if waiting := waitingEventsOf(t, h, instanceID); len(waiting) != 2 {
		t.Fatalf("the two steps are waiting for %v, want one event each", waiting)
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}
	if waiting := waitingEventsOf(t, h, instanceID); len(waiting) != 0 {
		t.Fatalf("the finished sub-process is still waiting for events on %v", waiting)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "await-documents"); len(left) != 0 {
		t.Fatalf("the finished sub-process left %d token(s) on a step inside it", len(left))
	}

	completeTaskAt(ctx, t, h, instanceID, "decide", nil)
	requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessCompleted)
}

// evidenceGathering is a step that is itself a sub-process — one interview —
// with a message boundary event on it that leads to closing the file.
func withEvidenceGathering(def *entities.ProcessDefinition) {
	research := def.Nodes[1]
	research.Nodes = append(research.Nodes,
		&entities.Node{
			ID: "gather-evidence", Type: entities.SubProcess, Name: "Gather the evidence", ParentID: "research",
			Nodes: []*entities.Node{
				{ID: "evidence-start", Type: entities.StartEvent, ParentID: "gather-evidence"},
				{ID: "interview-witness", Type: entities.UserTask, Name: "Interview the witness", ParentID: "gather-evidence"},
				{ID: "evidence-end", Type: entities.EndEvent, ParentID: "gather-evidence"},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "e1", SourceRef: "evidence-start", TargetRef: "interview-witness"},
				{ID: "e2", SourceRef: "interview-witness", TargetRef: "evidence-end"},
			},
		},
		&entities.Node{ID: "claim-withdrawn", Type: entities.BoundaryEvent, AttachedToRef: "gather-evidence",
			ParentID: "research", Properties: map[string]any{"message_name": "claim-withdrawn"}},
		&entities.Node{ID: "close-file", Type: entities.UserTask, Name: "Close the file", ParentID: "research"},
	)
	research.Flows = append(research.Flows,
		&entities.SequenceFlow{ID: "w1", SourceRef: "claim-withdrawn", TargetRef: "close-file"})
}

// BPMN 2.0.2 §10.3.5, §13.2.5: everything still running inside is cancelled —
// at whatever depth, with the events attached to it (§13.4.3). A step that is
// itself a sub-process holds no token while it runs; the steps inside it do,
// and its boundary events were armed when it was entered.
//
// Looking only at the steps directly inside found nothing to end: the
// interview stayed in somebody's inbox, its token stayed on the instance, and
// the instance could never end. Looking only at the steps holding a token left
// the boundary event armed: its message, arriving later, opened a task inside
// a sub-process the instance had left.
func TestAnAdHocSubProcessWithdrawsWhatIsRunningInsideAStepThatIsASubProcess(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Nested Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-nested", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	withEvidenceGathering(&def)
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-nested", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "gather-evidence"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	if open := h.openTasksOn(t, instanceID, "interview-witness"); open != 1 {
		t.Fatalf("gathering the evidence opened %d interview(s), want 1", open)
	}
	if waiting := waitingEventsOf(t, h, instanceID); len(waiting) != 1 || waiting[0] != "claim-withdrawn" {
		t.Fatalf("gathering the evidence is waiting for %v, want its one boundary event", waiting)
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}
	if open := h.openTasksOn(t, instanceID, "interview-witness"); open != 0 {
		t.Fatalf("%d task(s) of the finished sub-process are still in somebody's inbox", open)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "interview-witness"); len(left) != 0 {
		t.Fatalf("the finished sub-process left %d token(s) on a step inside it", len(left))
	}
	if waiting := waitingEventsOf(t, h, instanceID); len(waiting) != 0 {
		t.Fatalf("the finished sub-process is still waiting for events on %v", waiting)
	}

	// The message the boundary event was waiting for arrives afterwards.
	if err := h.svc.SendMessage(ctx, h.projID, "claim-withdrawn", "", nil); err != nil {
		t.Fatalf("send the message: %v", err)
	}
	if seen := tasksEverOn(t, h, instanceID, "close-file"); seen != 0 {
		t.Fatalf("a message for a sub-process that had ended opened %d task(s) inside it", seen)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "close-file"); len(left) != 0 {
		t.Fatalf("a message for a sub-process that had ended put %d token(s) inside it", len(left))
	}

	completeTaskAt(ctx, t, h, instanceID, "decide", nil)
	instance := requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessCompleted)
	if len(instance.Tokens) != 0 {
		t.Fatalf("the finished instance still holds %d token(s)", len(instance.Tokens))
	}
}

// BPMN 2.0.2 §10.3.5: cancelRemainingInstances set to false — what is still
// running is left to finish, at whatever depth it is running.
//
// A step that is itself a sub-process holds no token while it runs, so a
// sub-process that asked only its own steps saw nothing running and finished
// over the interview.
func TestAnAdHocSubProcessToldToKeepItsStepsWaitsForOneThatIsASubProcess(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Keep Nested Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-keeps-nested", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	def.Nodes[1].Properties = map[string]any{entities.CancelRemainingInstancesProperty: false}
	withEvidenceGathering(&def)
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-keeps-nested", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "gather-evidence"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	if h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the sub-process finished over a step it was told to wait for")
	}
	if open := h.openTasksOn(t, instanceID, "interview-witness"); open != 1 {
		t.Fatalf("the interview still running has %d open task(s), want it left alone", open)
	}

	completeTaskAt(ctx, t, h, instanceID, "interview-witness", nil)

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the last running step finished and the sub-process still did not")
	}
	if seen := tasksEverOn(t, h, instanceID, "decide"); seen != 1 {
		t.Fatalf("the process moved past the sub-process %d times, want once", seen)
	}
	if waiting := waitingEventsOf(t, h, instanceID); len(waiting) != 0 {
		t.Fatalf("the finished sub-process is still waiting for events on %v", waiting)
	}
}
