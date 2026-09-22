package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// parallelApproval is start → split → (branchA ‖ branchB) → join → end.
//
// The join's id is the parameter because a join gateway is where the engine
// keeps a counter of the branches that have arrived, and renaming it across a
// version is what used to leave that counter behind.
func parallelApproval(projectID uuid.UUID, joinID string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "parallel-approval",
		Name:    "Parallel approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "split", Type: entities.ParallelGateway, Incoming: []string{"f1"}, Outgoing: []string{"f2", "f3"}},
			{ID: "branchA", Name: "Branch A", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"f2"}, Outgoing: []string{"f4"}},
			{ID: "branchB", Name: "Branch B", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"f3"}, Outgoing: []string{"f5"}},
			{ID: joinID, Type: entities.ParallelGateway, Incoming: []string{"f4", "f5"}, Outgoing: []string{"f6"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f6"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "branchA"},
			{ID: "f3", SourceRef: "split", TargetRef: "branchB"},
			{ID: "f4", SourceRef: "branchA", TargetRef: joinID},
			{ID: "f5", SourceRef: "branchB", TargetRef: joinID},
			{ID: "f6", SourceRef: joinID, TargetRef: "end"},
		},
	}
}

func (f *fixture) deployParallel(t *testing.T, joinID string) uuid.UUID {
	t.Helper()
	id, err := f.svc.CreateDefinition(f.ctx, parallelApproval(f.project, joinID))
	if err != nil {
		t.Fatalf("deploy parallel approval: %v", err)
	}
	return id
}

// completeTaskOn completes the one open task sitting on nodeID.
func (f *fixture) completeTaskOn(t *testing.T, nodeID, actor string) {
	t.Helper()
	tasks, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.NodeID() != nodeID {
			continue
		}
		if err := f.svc.CompleteTask(f.ctx, task.ID, actor, nil); err != nil {
			t.Fatalf("complete the task on %q: %v", nodeID, err)
		}
		return
	}
	t.Fatalf("no open task on %q; there are %d open tasks", nodeID, len(tasks))
}

func (f *fixture) onlyInstance(t *testing.T) entities.ProcessInstance {
	t.Helper()
	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected exactly one instance, found %d", len(instances))
	}
	return instances[0]
}

// TestMigrationRekeysJoinCountersSoTheJoinStillCompletes is the deadlock.
//
// Root cause: the instance keeps a per-node count of the branches that have
// reached each waiting gateway, and the migration rewrote tokens, tasks and
// jobs but not that map — so a renamed join gateway started counting from zero
// and waited forever for a branch that had already arrived.
func TestMigrationRekeysJoinCountersSoTheJoinStillCompletes(t *testing.T) {
	f := newFixture(t)
	v1 := f.deployParallel(t, "join")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "parallel-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}

	// One branch arrives at the join and waits for the other. That arrival is
	// recorded against the node id "join" and nothing else.
	f.completeTaskOn(t, "branchA", "ada")

	v2 := f.deployParallel(t, "gate")
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"join": "gate"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The second branch arrives. With the counter carried across, that is the
	// second of two and the gateway fires.
	f.completeTaskOn(t, "branchB", "bo")

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("the instance is %q after both branches finished; the join is still waiting for a branch that already arrived",
			instance.Status)
	}
}

// twoCounters is start → split → (review ‖ sign) → join → end, where review
// runs once per item.
//
// It exists to put engine bookkeeping on two different nodes at once: a
// multi-instance counter on "review" and an arrival counter on "join". Two
// counters is what makes a merge onto one target node ambiguous, which is the
// case no mapping can express and the planner therefore has to refuse.
func twoCounters(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "two-counters",
		Name:    "Two counters",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "split", Type: entities.ParallelGateway, Incoming: []string{"f1"}, Outgoing: []string{"f2", "f3"}},
			{
				ID: "review", Name: "Review", Type: entities.UserTask, Assignee: "ada",
				MultiInstanceType: "parallel", Collection: "items", ElementVariable: "item",
				Incoming: []string{"f2"}, Outgoing: []string{"f4"},
			},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"f3"}, Outgoing: []string{"f5"}},
			{ID: "join", Type: entities.ParallelGateway, Incoming: []string{"f4", "f5"}, Outgoing: []string{"f6"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f6"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "review"},
			{ID: "f3", SourceRef: "split", TargetRef: "sign"},
			{ID: "f4", SourceRef: "review", TargetRef: "join"},
			{ID: "f5", SourceRef: "sign", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "end"},
		},
	}
}

// oneNode is start → gate → end: somewhere for a mapping to point, with no
// node of any of the source's names.
func oneNode(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "two-counters",
		Name:    "Two counters",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"g1"}},
			{ID: "gate", Name: "Gate", Type: entities.UserTask, Assignee: "cyd", Incoming: []string{"g1"}, Outgoing: []string{"g2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"g2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "g1", SourceRef: "start", TargetRef: "gate"},
			{ID: "g2", SourceRef: "gate", TargetRef: "end"},
		},
	}
}

// withTwoCounters starts an instance and leaves a part-finished multi-instance
// counter on "review" and an arrival counter on "join".
func (f *fixture) withTwoCounters(t *testing.T) uuid.UUID {
	t.Helper()
	v1, err := f.svc.CreateDefinition(f.ctx, twoCounters(f.project))
	if err != nil {
		t.Fatalf("deploy two-counters: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "two-counters", map[string]any{"items": []any{"x", "y"}}); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	// One of the two review iterations, so "review" holds a part-finished
	// multi-instance counter.
	f.completeTaskOn(t, "review", "ada")
	// The other branch, so "join" holds one arrival and no token.
	f.completeTaskOn(t, "sign", "bo")
	return v1
}

// TestMigrationSaysWhenItIsTheBookkeepingThatCannotLand.
//
// This engine parks a token on a waiting join as well as counting the arrival,
// so today the token check already refuses this mapping. The bookkeeping check
// is not therefore redundant: it is what states the actual reason, and it is
// the one that still holds if a join ever stops holding a token. A refusal that
// names only the token sends somebody to map a gateway and leaves them none the
// wiser about the counter riding on it.
func TestMigrationSaysWhenItIsTheBookkeepingThatCannotLand(t *testing.T) {
	f := newFixture(t)
	v1 := f.withTwoCounters(t)

	v2, err := f.svc.CreateDefinition(f.ctx, oneNode(f.project))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"review": "gate"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatalf("a migration that would drop the arrival counter held on \"join\" was reported as applicable; moves: %+v", plan.Moves)
	}
	var named bool
	for _, refusal := range plan.Refusals {
		if strings.Contains(refusal, "bookkeeping") && strings.Contains(refusal, "join") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the refusals do not say the engine bookkeeping on \"join\" is what cannot land: %v", plan.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"review": "gate"}); err == nil {
		t.Fatal("the apply accepted what the plan refused")
	}
}

// TestMigrationRefusesToMergeTwoCounters is the ambiguity nothing can resolve:
// adding two counters together invents progress that never happened, and
// keeping one loses progress that did.
func TestMigrationRefusesToMergeTwoCounters(t *testing.T) {
	f := newFixture(t)
	v1 := f.withTwoCounters(t)

	v2, err := f.svc.CreateDefinition(f.ctx, oneNode(f.project))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// Both stateful nodes onto one target.
	mapping := map[string]string{"review": "gate", "join": "gate"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a mapping that merges a multi-instance counter and a join counter onto one node was reported as applicable")
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err == nil {
		t.Fatal("the apply accepted what the plan refused")
	}
}

// TestAMigratedTaskTakesItsAuthorityFromTheNodeItLandsOn is the authorisation
// bug, and it is the reason this file exists.
//
// Root cause: a task that changed node kept every field it had, so the
// assignee, candidate groups and form of the deleted step travelled onto the
// step that replaced it — the person whose approval management had just removed
// could complete the approval management had kept.
func TestAMigratedTaskTakesItsAuthorityFromTheNodeItLandsOn(t *testing.T) {
	f := newFixture(t)

	quotation := func(nodeID, assignee, group string) *entities.ProcessDefinition {
		return &entities.ProcessDefinition{
			Project: &entities.Project{ID: f.project},
			Key:     "quotation-approval",
			Name:    "Quotation approval",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
				{
					ID: nodeID, Name: "Approve", Type: entities.UserTask,
					Assignee:        assignee,
					CandidateGroups: []*entities.Group{{Name: group}},
					Incoming:        []string{"f1"}, Outgoing: []string{"f2"},
				},
				{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: nodeID},
				{ID: "f2", SourceRef: nodeID, TargetRef: "end"},
			},
		}
	}

	v1, err := f.svc.CreateDefinition(f.ctx, quotation("opsApprove", "ops-manager", "operations"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, quotation("salesApprove", "sales-manager", "sales"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"opsApprove": "salesApprove"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	tasks, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task after the migration, found %d", len(tasks))
	}
	task := tasks[0]
	if task.NodeID() != "salesApprove" {
		t.Fatalf("the task is on %q; it was mapped onto salesApprove", task.NodeID())
	}
	if task.Assignee == nil || task.Assignee.Username != "sales-manager" {
		got := "<nobody>"
		if task.Assignee != nil {
			got = task.Assignee.Username
		}
		t.Fatalf("the migrated task is assigned to %q; the node it now sits on is the sales manager's, "+
			"so the operations manager must not still hold it", got)
	}
	var groups []string
	for _, group := range task.CandidateGroups {
		if group != nil {
			groups = append(groups, group.Name)
		}
	}
	if len(groups) != 1 || groups[0] != "sales" {
		t.Fatalf("the migrated task offers candidate groups %v; they must come from the node it landed on", groups)
	}
}

// TestMigrationIsRecordedOnTheInstanceTimeline: without this the trail shows a
// task completed on a node the instance was never started on, and nothing
// explains how it got there.
func TestMigrationIsRecordedOnTheInstanceTimeline(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	instanceID, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"approve": "review"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := f.svc.GetAuditLogs(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == impl.EventInstanceMigrated {
			if entry.Narrative == "" {
				t.Error("the migration was recorded with no narrative; the trail is read by people")
			}
			return
		}
	}
	t.Fatalf("the instance changed version and the trail does not say so; %d entries and none is %q",
		len(entries), impl.EventInstanceMigrated)
}

// TestThePlanNamesWhatTheNewVersionRemoved. A removed user task is also the
// removed producer of everything its form used to write, so which nodes went
// away is the first thing a reviewer needs.
func TestThePlanNamesWhatTheNewVersionRemoved(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"approve": "review"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.RemovedNodes) != 1 || plan.RemovedNodes[0] != "approve" {
		t.Fatalf("the plan says the removed nodes are %v; version 2 dropped \"approve\"", plan.RemovedNodes)
	}
}

// TestThePlanCountsClaimedWorkApart. An unclaimed task is a queue item; a
// claimed one is a person with the form open who is about to lose their place.
func TestThePlanCountsClaimedWorkApart(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"approve": "review"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, move := range plan.Moves {
		if move.From != "approve" {
			continue
		}
		// The fixture's node names an assignee, so the task is created claimed.
		if move.TasksClaimed != 1 {
			t.Fatalf("the plan shows %d claimed tasks on approve; the node assigns one to ada", move.TasksClaimed)
		}
		if len(plan.Warnings) == 0 {
			t.Fatal("a claimed task would be returned to the queue and the plan warns about nothing")
		}
		return
	}
	t.Fatalf("the plan does not mention approve: %+v", plan.Moves)
}

// TestMigrationRefusesToDetachABoundaryEventFromItsActivity. A three-day
// escalation on "approve" is not a three-day escalation on whatever else the
// target happens to attach one to.
func TestMigrationRefusesToDetachABoundaryEventFromItsActivity(t *testing.T) {
	f := newFixture(t)

	guarded := func(taskID, boundaryID, hostID string) *entities.ProcessDefinition {
		return &entities.ProcessDefinition{
			Project: &entities.Project{ID: f.project},
			Key:     "guarded-approval",
			Name:    "Guarded approval",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
				{ID: taskID, Name: "Approve", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
				{ID: "escalate", Name: "Escalate", Type: entities.UserTask, Assignee: "bo"},
				{
					ID: boundaryID, Type: entities.BoundaryEvent, AttachedToRef: hostID,
					Properties: map[string]any{"event_type": "timer", "timer_duration": "P3D"},
				},
				{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: taskID},
				{ID: "f2", SourceRef: taskID, TargetRef: "end"},
			},
		}
	}

	// v1 guards "approve"; v2 guards "escalate" instead.
	v1, err := f.svc.CreateDefinition(f.ctx, guarded("approve", "timeout", "approve"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "guarded-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, guarded("review", "delay", "escalate"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{
		"approve": "review",
		"timeout": "delay",
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a mapping that moves a boundary event onto a different activity was reported as applicable")
	}
}

// controlled is start → approve → end, where "approve" may be marked as
// carrying a control obligation.
func controlled(projectID uuid.UUID, nodeID string, controlled bool) *entities.ProcessDefinition {
	properties := map[string]any{}
	if controlled {
		properties["compliance_relevant"] = true
		properties["compliance_note"] = "second signature required over $50k"
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "controlled-approval",
		Name:    "Controlled approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"c1"}},
			{
				ID: nodeID, Name: "Approve", Type: entities.UserTask, Assignee: "ada",
				Properties: properties,
				Incoming:   []string{"c1"}, Outgoing: []string{"c2"},
			},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"c2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: nodeID},
			{ID: "c2", SourceRef: nodeID, TargetRef: "end"},
		},
	}
}

// TestDroppingAControlStepIsHeldUntilSomebodyAcceptsIt.
//
// The difference between "this approval was skipped" and "this approval was
// skipped, by a named person, who was told what it was for" is the whole of
// what an auditor asks for six months later, and only a deliberate
// acknowledgement can produce the second.
func TestDroppingAControlStepIsHeldUntilSomebodyAcceptsIt(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "opsApprove", true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "salesApprove", false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	mapping := map[string]string{"opsApprove": "salesApprove"}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a migration that drops a control-bearing step was applicable with nobody accepting it")
	}
	if len(plan.ComplianceHolds) != 1 || plan.ComplianceHolds[0].NodeID != "opsApprove" {
		t.Fatalf("the plan does not hold on the control step: %+v", plan.ComplianceHolds)
	}
	if plan.ComplianceHolds[0].Note == "" {
		t.Error("the hold carries no note; \"you are skipping opsApprove\" and \"you are skipping the second signature\" are different sentences")
	}
	if plan.ComplianceHolds[0].Instances != 1 {
		t.Errorf("the hold covers %d instances; one has not passed the step", plan.ComplianceHolds[0].Instances)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err == nil {
		t.Fatal("the apply accepted what the plan held")
	}

	// Accepted by name, it goes through — and says who accepted it.
	accepted := []servicecontracts.MigrationOption{
		servicecontracts.WithAcknowledgedHolds("opsApprove"),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping, accepted...); err != nil {
		t.Fatalf("an acknowledged hold was still refused: %v", err)
	}

	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	entries, err := f.svc.GetAuditLogs(f.ctx, instances[0].ID)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type != impl.EventInstanceMigrated {
			continue
		}
		if !strings.Contains(entry.Narrative, "dita") {
			t.Errorf("the trail does not name who authorised the waiver: %q", entry.Narrative)
		}
		if !strings.Contains(entry.Narrative, "opsApprove") {
			t.Errorf("the trail does not name the control that was waived: %q", entry.Narrative)
		}
		return
	}
	t.Fatal("no migration entry on the trail")
}

// TestAnUnmarkedStepIsNotHeld keeps the gate narrow: this is additive, and a
// definition that marks nothing behaves exactly as it did before.
func TestAnUnmarkedStepIsNotHeld(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "opsApprove", false))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "salesApprove", false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"opsApprove": "salesApprove"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.ComplianceHolds) != 0 {
		t.Fatalf("an unmarked step produced a hold: %+v", plan.ComplianceHolds)
	}
	if !plan.Applicable() {
		t.Fatalf("an ordinary migration was refused: %v", plan.Refusals)
	}
}

// TestAnInstanceThatAlreadyPassedTheControlIsNotHeld. The three populations
// that arrive at a removed approval are not one question: an instance that
// already gave the approval waived nothing.
func TestAnInstanceThatAlreadyPassedTheControlIsNotHeld(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.CreateDefinition(f.ctx, controlledTwoStep(f.project))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	// The control step is performed, so this instance is past it.
	f.completeTaskOn(t, "opsApprove", "ada")

	v2, err := f.svc.CreateDefinition(f.ctx, controlledTwoStepWithout(f.project))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.ComplianceHolds) != 0 {
		t.Fatalf("an instance that already gave the approval was held as though it had not: %+v", plan.ComplianceHolds)
	}
	if !plan.Applicable() {
		t.Fatalf("migrating an instance past the control was refused: %v", plan.Refusals)
	}
}

// controlledTwoStep is start → opsApprove (controlled) → salesApprove → end.
func controlledTwoStep(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "controlled-two-step",
		Name:    "Controlled two step",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"t1"}},
			{
				ID: "opsApprove", Name: "Operations approve", Type: entities.UserTask, Assignee: "ada",
				Properties: map[string]any{"compliance_relevant": true},
				Incoming:   []string{"t1"}, Outgoing: []string{"t2"},
			},
			{ID: "salesApprove", Name: "Sales approve", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"t2"}, Outgoing: []string{"t3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"t3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "t1", SourceRef: "start", TargetRef: "opsApprove"},
			{ID: "t2", SourceRef: "opsApprove", TargetRef: "salesApprove"},
			{ID: "t3", SourceRef: "salesApprove", TargetRef: "end"},
		},
	}
}

// controlledTwoStepWithout is the same process with the control step deleted —
// management's change, in one definition.
func controlledTwoStepWithout(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "controlled-two-step",
		Name:    "Controlled two step",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"t1"}},
			{ID: "salesApprove", Name: "Sales approve", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"t1"}, Outgoing: []string{"t3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"t3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "t1", SourceRef: "start", TargetRef: "salesApprove"},
			{ID: "t3", SourceRef: "salesApprove", TargetRef: "end"},
		},
	}
}
