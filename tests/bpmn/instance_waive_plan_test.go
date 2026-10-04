package bpmn_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A plan shows the administrator the work the command would act on, and its
// visit key is the identity of that work: the same for as long as the work is,
// whoever claims it and whatever else the instance learns, and different the
// moment the work is.
func TestThePlanNamesTheWorkAndItsKeyChangesOnlyWhenTheWorkDoes(t *testing.T) {
	h := newEngineHarness(t, "Plan Work Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "plan-ops"), nil)
	task := openIterationTasks(ctx, t, h, id, "opsApprove")[0]

	waive := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true})
	plan := w.preview(t, waive)
	want := entities.DeviationPlan{
		InstanceID: id, Kind: entities.DeviationWaive, Scope: entities.DeviationScopeTask,
		NodeID: "opsApprove", NodeName: "Operations approve", VisitKey: plan.VisitKey,
		OpenWork: []entities.DeviationOpenWork{{TaskID: task.ID, Name: "Operations approve", Status: entities.TaskClaimed, Assignee: "ollie"}},
		Outputs:  map[string]any{"approved": true},
		Warnings: []string{"“Operations approve” is with ollie, who will be told it was withdrawn."},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("the plan for a waive:\n got  %+v\n want %+v", plan, want)
	}
	if !strings.HasPrefix(plan.VisitKey, "dv1-") || len(plan.VisitKey) != 36 {
		t.Fatalf("the visit key %q: want dv1- and 32 characters", plan.VisitKey)
	}

	hold := w.preview(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	cancel := w.preview(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
	if hold.Scope != entities.DeviationScopeInstance || cancel.Scope != entities.DeviationScopeInstance {
		t.Errorf("a hold reaches the %s and a cancel the %s; both reach the instance", hold.Scope, cancel.Scope)
	}
	if !reflect.DeepEqual(hold.OpenWork, want.OpenWork) || !reflect.DeepEqual(cancel.OpenWork, want.OpenWork) {
		t.Errorf("the open work of a hold %+v and of a cancel %+v, want the step's one task", hold.OpenWork, cancel.OpenWork)
	}
	if hold.VisitKey == plan.VisitKey || cancel.VisitKey == plan.VisitKey || hold.VisitKey == cancel.VisitKey {
		t.Errorf("a waive, a hold and a cancel of one visit share a key: %s, %s, %s", plan.VisitKey, hold.VisitKey, cancel.VisitKey)
	}

	// What is not the work: asking again, another reason, another answer for
	// the waiver to count as, a variable somebody else set.
	reworded := waive
	reworded.Reason = "A different reason for the same work."
	reworded.Outputs = map[string]any{"approved": false}
	if again := w.preview(t, reworded); again.VisitKey != plan.VisitKey {
		t.Error("another reason and other outputs changed the key; they are the request, not the work")
	}
	instance, err := h.engine.GetInstance(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	instance.SetVariable("note", "left by somebody else")
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("set a variable: %v", err)
	}
	if again := w.preview(t, waive); again.VisitKey != plan.VisitKey {
		t.Error("a variable set on the instance changed the key")
	}

	// The holder does the work: the step is not this visit any more.
	if err := h.svc.CompleteTask(ctx, task.ID, "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("ollie completes: %v", err)
	}
	if moved := w.preview(t, waive); moved.VisitKey == plan.VisitKey || moved.Applicable() {
		t.Errorf("the step was completed and the plan for it has the same key (%v) or still applies (%v)",
			moved.VisitKey == plan.VisitKey, moved.Applicable())
	}

	// A claim is not different work: the same task, with somebody's name on it.
	sales := deviationCommand(entities.DeviationWaive, id, "salesApprove", nil)
	unclaimed := w.preview(t, sales)
	if len(unclaimed.OpenWork) != 1 || unclaimed.OpenWork[0].Status != entities.TaskUnclaimed || unclaimed.OpenWork[0].Assignee != "" || len(unclaimed.Warnings) != 0 {
		t.Fatalf("the plan for a task nobody has taken: open work %+v, warnings:%s", unclaimed.OpenWork, lines(unclaimed.Warnings))
	}
	if err := h.svc.ClaimTask(testutils.AsOperator(ctx, "olga"), unclaimed.OpenWork[0].TaskID, "olga"); err != nil {
		t.Fatalf("olga claims: %v", err)
	}
	claimed := w.preview(t, sales)
	if claimed.VisitKey != unclaimed.VisitKey {
		t.Error("a claim changed the key: a preview could never be applied on work somebody is picking up")
	}
	if len(claimed.OpenWork) != 1 || claimed.OpenWork[0].Status != entities.TaskClaimed || claimed.OpenWork[0].Assignee != "olga" ||
		!said(claimed.Warnings, "“Sales approve” is with olga, who will be told it was withdrawn.") {
		t.Errorf("the plan after the claim: open work %+v, warnings:%s", claimed.OpenWork, lines(claimed.Warnings))
	}
}

// BPMN 2.0.2 §13.2.7 (Multiple Instances Activity). A repeating step is one
// step with a run for each person: the plan lists every run still open, and a
// run finished since the preview is different work.
func TestThePlanForARepeatingStepListsEveryOpenRun(t *testing.T) {
	h := newEngineHarness(t, "Plan Runs Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := startApproval(t, h, approvalDefinition(h.projID, "plan-parallel", "parallel", ""), "ana", "budi", "citra")
	waive := deviationCommand(entities.DeviationWaive, id, "approve", nil)

	plan := w.preview(t, waive)
	if !plan.Applicable() {
		t.Fatalf("refused:%s", lines(plan.Refusals))
	}
	var runs, ids []string
	for _, work := range plan.OpenWork {
		runs = append(runs, work.IterationID)
		ids = append(ids, work.TaskID.String())
	}
	if !slices.IsSorted(ids) {
		t.Errorf("the open work is not in the order of its ids, so two previews of it could read differently: %v", ids)
	}
	slices.Sort(runs)
	if !reflect.DeepEqual(runs, []string{"0", "1", "2"}) {
		t.Fatalf("the open work lists the runs %v, want all three", runs)
	}
	// The step's own list is not a decision point of its own waive: it is
	// ended whole and never asks for the list again.
	if len(plan.DecisionPoints) != 0 {
		t.Errorf("decision points %+v, want none", plan.DecisionPoints)
	}

	open := openIterationTasks(ctx, t, h, id, "approve")
	if err := completeAs(ctx, h, open[0], "carol", map[string]any{"decision": "yes"}); err != nil {
		t.Fatalf("complete one approval: %v", err)
	}
	after := w.preview(t, waive)
	if len(after.OpenWork) != 2 || after.VisitKey == plan.VisitKey {
		t.Fatalf("after one run finished: %d open, the key changed: %v; want two and a different key",
			len(after.OpenWork), after.VisitKey != plan.VisitKey)
	}
}

// claimWithAGateway is start → Review the claim (declares approved) →
// Approved? → pay | rework.
func claimWithAGateway(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key, Name: "Claim",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved")},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Approved?"},
			{ID: "pay", Type: entities.UserTask, Name: "Pay the claim"},
			{ID: "rework", Type: entities.UserTask, Name: "Rework the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "pay", Condition: "approved"},
			{ID: "no", SourceRef: "decide", TargetRef: "rework", Condition: "approved = false"},
			{ID: "f3", SourceRef: "pay", TargetRef: "end"},
			{ID: "f4", SourceRef: "rework", TargetRef: "end"},
		},
	}
}

// AGENTS.md §0: no silent default at a decision point. A plan refuses a waive
// that leaves a gateway to decide from a value nobody gave, and says which
// value in words; the value the instance holds from before does not count.
func TestThePlanRefusesAWaiveThatLeavesADecisionToAValueNobodyGave(t *testing.T) {
	h := newEngineHarness(t, "Plan Decision Project")
	w := newWaiver(h)
	id := w.start(t, claimWithAGateway(h.projID, "plan-gateway"), map[string]any{"approved": true})

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", nil))
	want := "“Approved?” decides from approved, which “Review the claim” would have set; say what the waiver counts as by supplying approved."
	if !reflect.DeepEqual(plan.Refusals, []string{want}) {
		t.Fatalf("the plan's refusals:%s\nwant only\n  %s", lines(plan.Refusals), want)
	}
	point, listed := pointOfKind(plan, "decide", entities.DecisionPointGateway)
	if !listed || !reflect.DeepEqual(point.Missing, []string{"approved"}) || len(point.Supplied) != 0 || !point.Analysed {
		t.Fatalf("the gateway's decision point: %+v (listed %v)", point, listed)
	}

	supplied := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": false}))
	point, _ = pointOfKind(supplied, "decide", entities.DecisionPointGateway)
	if !supplied.Applicable() || !reflect.DeepEqual(point.Supplied, []string{"approved"}) || len(point.Missing) != 0 {
		t.Fatalf("with the value given: refusals:%s\nthe point: %+v", lines(supplied.Refusals), point)
	}

	// A hold and a cancel set nothing, so nothing is asked of them.
	for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationCancel} {
		if other := w.preview(t, deviationCommand(kind, id, "review", nil)); !other.Applicable() || len(other.DecisionPoints) != 0 {
			t.Errorf("a %s: refusals:%s\ndecision points %+v", kind, lines(other.Refusals), other.DecisionPoints)
		}
	}
}

// A decision table is read at the version the step consults — the one in
// force, or the one it pins — together with every decision that one requires.
// One nobody stored cannot be read, and the plan says so instead of saying it
// reads nothing.
func TestThePlanReadsEachDecisionAtTheVersionTheStepConsults(t *testing.T) {
	h := newEngineHarness(t, "Plan Decisions Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	table := func(key string, reads string, requires ...string) entities.DecisionDefinition {
		return entities.DecisionDefinition{
			Project: &entities.Project{ID: h.projID}, Key: key, Name: key, RequiredDecisions: requires,
			Inputs:  []entities.DecisionInput{{ID: "in1", Label: reads, Expression: reads, Type: "string"}},
			Outputs: []entities.DecisionOutput{{ID: "out1", Label: "Answer", Name: key + "Answer", Type: "string"}},
			Rules:   []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"yes"}}},
		}
	}
	first, err := h.svc.CreateDecision(ctx, table("discount", "approved"))
	if err != nil {
		t.Fatalf("store the discount decision: %v", err)
	}
	staged, err := h.svc.UpdateDecision(ctx, first, table("discount", "amount"), false)
	if err != nil || staged.Version != 2 || staged.Live {
		t.Fatalf("stage a second version: %+v, %v", staged, err)
	}
	for _, d := range []entities.DecisionDefinition{table("risk", "amount"), table("tier", "approved", "risk")} {
		if _, err := h.svc.CreateDecision(ctx, d); err != nil {
			t.Fatalf("store %s: %v", d.Key, err)
		}
	}

	rule := func(id, name string, properties map[string]any) *entities.Node {
		return &entities.Node{ID: id, Type: entities.BusinessRuleTask, Name: name, Properties: properties}
	}
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-decisions",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved", "amount")},
			rule("inForce", "Apply the discount in force", map[string]any{"decision_key": "discount"}),
			rule("pinned", "Apply the discount being tried", map[string]any{"decision_key": "discount", "decision_version": 2}),
			rule("chained", "Decide the tier", map[string]any{"decision_key": "tier"}),
			rule("nobodyStored", "Ask a decision nobody stored", map[string]any{"decision_key": "no-such-decision"}),
			rule("noSuchVersion", "Apply a version nobody stored", map[string]any{"decision_key": "discount", "decision_version": 9}),
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "inForce"},
			{ID: "f3", SourceRef: "inForce", TargetRef: "pinned"},
			{ID: "f4", SourceRef: "pinned", TargetRef: "chained"},
			{ID: "f5", SourceRef: "chained", TargetRef: "nobodyStored"},
			{ID: "f6", SourceRef: "nobodyStored", TargetRef: "noSuchVersion"},
			{ID: "f7", SourceRef: "noSuchVersion", TargetRef: "end"},
		},
	}, nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	for nodeID, want := range map[string]entities.DecisionPoint{
		"inForce":       {Reads: []string{"approved"}, Supplied: []string{"approved"}, Analysed: true},
		"pinned":        {Reads: []string{"amount"}, Missing: []string{"amount"}, Analysed: true},
		"chained":       {Reads: []string{"amount", "approved"}, Supplied: []string{"approved"}, Missing: []string{"amount"}, Analysed: true},
		"nobodyStored":  {Analysed: false},
		"noSuchVersion": {Analysed: false},
	} {
		point, listed := pointOfKind(plan, nodeID, entities.DecisionPointDecisionTable)
		if !listed {
			t.Errorf("%s is not among the decision points %+v", nodeID, plan.DecisionPoints)
			continue
		}
		if !reflect.DeepEqual(point.Reads, want.Reads) || !reflect.DeepEqual(point.Supplied, want.Supplied) ||
			!reflect.DeepEqual(point.Missing, want.Missing) || point.Analysed != want.Analysed {
			t.Errorf("%s: reads %v, supplied %v, missing %v, analysed %v\n want reads %v, supplied %v, missing %v, analysed %v",
				nodeID, point.Reads, point.Supplied, point.Missing, point.Analysed, want.Reads, want.Supplied, want.Missing, want.Analysed)
		}
	}
	wantRefusals := []string{
		"“Decide the tier” decides from amount, which “Review the claim” would have set; say what the waiver counts as by supplying amount.",
		"“Apply the discount being tried” decides from amount, which “Review the claim” would have set; say what the waiver counts as by supplying amount.",
	}
	if !reflect.DeepEqual(plan.Refusals, wantRefusals) {
		t.Errorf("the refusals:%s\nwant:%s", lines(plan.Refusals), lines(wantRefusals))
	}
	for _, want := range []string{
		"“Ask a decision nobody stored” could not be read to see what it decides from; check it before applying.",
		"“Apply a version nobody stored” could not be read to see what it decides from; check it before applying.",
	} {
		if !said(plan.Warnings, want) {
			t.Errorf("the plan does not warn\n  %s\nits warnings:%s", want, lines(plan.Warnings))
		}
	}

	// The staged version is put into force: the step that pins nothing now
	// reads what that version reads.
	if err := h.svc.PromoteDecisionVersion(ctx, h.projID, "discount", 2); err != nil {
		t.Fatalf("put version 2 into force: %v", err)
	}
	promoted := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	if point, _ := pointOfKind(promoted, "inForce", entities.DecisionPointDecisionTable); !reflect.DeepEqual(point.Missing, []string{"amount"}) {
		t.Errorf("after version 2 came into force the step that pins nothing is missing %v, want amount", point.Missing)
	}
	everything := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true, "amount": 900}))
	if !everything.Applicable() {
		t.Errorf("with every value given:%s", lines(everything.Refusals))
	}
}

// Two more places a waived step's value goes: a process the instance calls,
// which is handed it and is not read, and a later step that repeats once for
// each item of a list the waived step would have made. The first is a warning
// and never a refusal; the second is refused until the list is given.
func TestThePlanListsWhatACalledProcessIsHandedAndTheListAStepRepeatsOver(t *testing.T) {
	h := newEngineHarness(t, "Plan Kinds Project")
	w := newWaiver(h)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-kinds-called",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "look", Type: entities.UserTask, Name: "Look the supplier up"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "look"}, {ID: "c2", SourceRef: "look", TargetRef: "end"}},
	})
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-kinds",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "pick", Type: entities.UserTask, Name: "Pick the reviewers", Assignee: "rita", Properties: testutils.FormDeclaring("reviewers", "urgent")},
			{ID: "ask", Type: entities.UserTask, Name: "Ask each reviewer", MultiInstanceType: "parallel", Collection: "reviewers", ElementVariable: "reviewer"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": "plan-kinds-called"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "pick"},
			{ID: "f2", SourceRef: "pick", TargetRef: "ask"},
			{ID: "f3", SourceRef: "ask", TargetRef: "check"},
			{ID: "f4", SourceRef: "check", TargetRef: "end"},
		},
	}, nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "pick", nil))
	list, listed := pointOfKind(plan, "ask", entities.DecisionPointCollection)
	if !listed || !list.Analysed || !reflect.DeepEqual(list.Missing, []string{"reviewers"}) {
		t.Fatalf("the list the next step repeats over: %+v (listed %v)", list, listed)
	}
	called, listed := pointOfKind(plan, "check", entities.DecisionPointCalledProcess)
	if !listed || called.Analysed || len(called.Missing) != 0 || !reflect.DeepEqual(called.Reads, []string{"reviewers", "urgent"}) {
		t.Fatalf("the called process: %+v (listed %v)", called, listed)
	}
	wantRefusal := "“Ask each reviewer” decides from reviewers, which “Pick the reviewers” would have set; say what the waiver counts as by supplying reviewers."
	if !reflect.DeepEqual(plan.Refusals, []string{wantRefusal}) {
		t.Errorf("the refusals:%s\nwant only\n  %s", lines(plan.Refusals), wantRefusal)
	}
	wantWarning := "“Check the supplier” starts another process and hands it reviewers and urgent, which “Pick the reviewers” would have set; " +
		"that process was not read, so check what it does with them before applying."
	if !said(plan.Warnings, wantWarning) {
		t.Errorf("the plan does not warn\n  %s\nits warnings:%s", wantWarning, lines(plan.Warnings))
	}

	// With the list given nothing refuses, and the called process is still
	// something to look at: nobody read it.
	given := w.preview(t, deviationCommand(entities.DeviationWaive, id, "pick", map[string]any{"reviewers": []any{"rita", "sam"}}))
	if !given.Applicable() || !said(given.Warnings, wantWarning) {
		t.Errorf("with the list given: refusals:%s\nwarnings:%s", lines(given.Refusals), lines(given.Warnings))
	}
}

// What a plan warns of, word for word: whose work an apply would take, what
// it could not read, and a hold that finds the step already held.
func TestThePlanWarnsOfWhatAnApplyWouldTakeAndWhatNobodyCouldRead(t *testing.T) {
	h := newEngineHarness(t, "Plan Warnings Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "warnings-ops"), nil)

	taken := "“Operations approve” is with ollie, who will be told it was withdrawn."
	for _, command := range []entities.DeviationCommand{
		deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true}),
		deviationCommand(entities.DeviationCancel, id, "opsApprove", nil),
	} {
		if plan := w.preview(t, command); !reflect.DeepEqual(plan.Warnings, []string{taken}) {
			t.Errorf("a %s warns:%s\nwant only\n  %s", command.Kind, lines(plan.Warnings), taken)
		}
	}
	// A hold takes nothing from anybody.
	hold := deviationCommand(entities.DeviationHold, id, "opsApprove", nil)
	if plan := w.preview(t, hold); len(plan.Warnings) != 0 || !plan.Applicable() {
		t.Errorf("a hold of a step nobody has held: warnings:%s\nrefusals:%s", lines(plan.Warnings), lines(plan.Refusals))
	}

	// An incident open on the step, as a migration's hold leaves one. It is
	// written through the repository: nothing in place can raise one until
	// the hold itself is applied.
	instance, err := h.repo.Process().Get(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	incident := models.IncidentModel{InstanceID: instance.ID, DefinitionID: instance.DefinitionID, NodeID: "opsApprove",
		Status: models.IncidentOpen, Error: "held by a migration"}
	incident.ID = models.UUID(uuid.Must(uuid.NewV7()))
	if _, err := h.repo.Incident().Create(ctx, incident); err != nil {
		t.Fatalf("raise the incident: %v", err)
	}
	held := "“Operations approve” already has an open incident; the hold will use it."
	if plan := w.preview(t, hold); !reflect.DeepEqual(plan.Warnings, []string{held}) || !plan.Applicable() {
		t.Errorf("a hold of a step already held warns:%s\nwant only\n  %s", lines(plan.Warnings), held)
	}
	if plan := w.preview(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil)); said(plan.Warnings, held) {
		t.Error("a cancel was told about the incident a hold would use")
	}

	// A gateway that routes by a script nobody here can read.
	scripted := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "warnings-script",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved")},
			{ID: "route", Type: entities.ExclusiveGateway, Name: "Route by script"},
			{ID: "work", Type: entities.UserTask, Name: "Do the work"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "route"},
			{ID: "js", SourceRef: "route", TargetRef: "work", Condition: "js:approved === true"},
			{ID: "plain", SourceRef: "route", TargetRef: "work", Condition: "approved = false"},
			{ID: "f3", SourceRef: "work", TargetRef: "end"},
		},
	}, nil)
	plan := w.preview(t, deviationCommand(entities.DeviationWaive, scripted, "review", map[string]any{"approved": true}))
	unread := "“Route by script” could not be read to see what it decides from; check it before applying."
	if !plan.Applicable() || !said(plan.Warnings, unread) {
		t.Errorf("a gateway routed by a script: refusals:%s\nwarnings:%s\nwant the warning\n  %s", lines(plan.Refusals), lines(plan.Warnings), unread)
	}
}

// callerOf is start → Have it checked (runs the process called) → end.
func callerOf(projectID uuid.UUID, key, called string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "haveItChecked", Type: entities.CallActivity, Name: "Have it checked", Properties: map[string]any{"called_process_key": called}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "p1", SourceRef: "start", TargetRef: "haveItChecked"}, {ID: "p2", SourceRef: "haveItChecked", TargetRef: "end"}},
	}
}

// theOneCalledBy is the one process an instance has started.
func theOneCalledBy(t *testing.T, h engineHarness, caller uuid.UUID) uuid.UUID {
	t.Helper()
	called, err := h.svc.ListSubProcesses(h.Ctx(), caller)
	if err != nil || len(called) != 1 {
		t.Fatalf("called processes: %d (err %v)", len(called), err)
	}
	return called[0].ID
}

// Design §7.4, rules 11 and 12. A called instance that is waiting is part of a
// larger process, and an instance waiting on one it started cannot be ended
// under it: the plan refuses both, names the other instance, and lists what is
// still running.
func TestThePlanForACancelSaysWhatElseDependsOnTheInstance(t *testing.T) {
	h := newEngineHarness(t, "Plan Cancel Project")
	w := newWaiver(h)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-cancel-called",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "review"}, {ID: "c2", SourceRef: "review", TargetRef: "end"}},
	})
	caller := w.start(t, callerOf(h.projID, "plan-cancel-caller", "plan-cancel-called"), nil)
	called := theOneCalledBy(t, h, caller)

	around := w.preview(t, deviationCommand(entities.DeviationCancel, caller, "haveItChecked", nil))
	want := "This instance is waiting on 1 process(es) it started (" + called.String() + "); cancel or finish those first."
	if !reflect.DeepEqual(around.Refusals, []string{want}) || !reflect.DeepEqual(around.CalledInstances, []uuid.UUID{called}) {
		t.Errorf("cancelling around a called instance: called %v, refusals:%s\nwant only\n  %s", around.CalledInstances, lines(around.Refusals), want)
	}
	inside := w.preview(t, deviationCommand(entities.DeviationCancel, called, "review", nil))
	want = "This instance was started by another process (instance " + caller.String() + "); cancel that one, or hold this one."
	if !reflect.DeepEqual(inside.Refusals, []string{want}) || len(inside.CalledInstances) != 0 {
		t.Errorf("cancelling a called instance: called %v, refusals:%s\nwant only\n  %s", inside.CalledInstances, lines(inside.Refusals), want)
	}
	// Either can be held, and the step inside the called one can be waived.
	for what, command := range map[string]entities.DeviationCommand{
		"holding the caller":                     deviationCommand(entities.DeviationHold, caller, "haveItChecked", nil),
		"holding the called instance":            deviationCommand(entities.DeviationHold, called, "review", nil),
		"waiving the step in the called process": deviationCommand(entities.DeviationWaive, called, "review", nil),
	} {
		if plan := w.preview(t, command); !plan.Applicable() || len(plan.CalledInstances) != 0 {
			t.Errorf("%s: called %v, refusals:%s", what, plan.CalledInstances, lines(plan.Refusals))
		}
	}
}

// Rulings addendum §10 and §13. A cancel may name no step, and one that does
// not is about the whole instance. While the instance waits anywhere the plan
// refuses it and says where, each step once, so that the record of a cancel
// names where the instance stood whenever it stood somewhere.
func TestACancelThatNamesNoStepIsToldWhereTheInstanceWaits(t *testing.T) {
	h := newEngineHarness(t, "Plan Cancel Where Project")
	w := newWaiver(h)
	branches := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-branches",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "stock", Type: entities.UserTask, Name: "Check stock", Assignee: "sam"},
			{ID: "credit", Type: entities.UserTask, Name: "Check credit", Assignee: "cara"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "stock"},
			{ID: "f3", SourceRef: "fork", TargetRef: "credit"},
			{ID: "f4", SourceRef: "stock", TargetRef: "join"},
			{ID: "f5", SourceRef: "credit", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "end"},
		},
	}, nil)
	plan := w.preview(t, deviationCommand(entities.DeviationCancel, branches, "", nil))
	want := "This instance is waiting at “Check credit” and “Check stock”; say which of those steps it is to be ended at."
	if !reflect.DeepEqual(plan.Refusals, []string{want}) {
		t.Errorf("the refusals:%s\nwant only\n  %s", lines(plan.Refusals), want)
	}
	if plan.NodeID != "" || plan.NodeName != "" || plan.Scope != entities.DeviationScopeInstance || len(plan.OpenWork) != 2 || plan.VisitKey == "" {
		t.Errorf("the plan of a cancel that names no step: %+v", plan)
	}
	if named := w.preview(t, deviationCommand(entities.DeviationCancel, branches, "stock", nil)); !named.Applicable() || named.NodeName != "Check stock" {
		t.Errorf("naming one of the steps: %q, refusals:%s", named.NodeName, lines(named.Refusals))
	}

	// Three runs of one step are one place the instance waits.
	approval := startApproval(t, h, approvalDefinition(h.projID, "plan-where-runs", "parallel", ""), "ana", "budi", "citra")
	plan = w.preview(t, deviationCommand(entities.DeviationCancel, approval, "", nil))
	want = "This instance is waiting at “Approve the purchase”; say which of those steps it is to be ended at."
	if !reflect.DeepEqual(plan.Refusals, []string{want}) || len(plan.OpenWork) != 3 {
		t.Errorf("%d open, refusals:%s\nwant three open and only\n  %s", len(plan.OpenWork), lines(plan.Refusals), want)
	}
}

// nothingFollows is start → Check the order, a step with no way out. The
// validator accepts it, and the engine ends an instance only at an end event:
// completing the step takes the token off and leaves the instance active and
// waiting nowhere — the state docs/upgrading.md calls "with nothing left",
// reached the way production reaches it.
func nothingFollows(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key, Name: "Order check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.UserTask, Name: "Check the order", Assignee: "rita"},
		},
		Flows: []*entities.SequenceFlow{{ID: "d1", SourceRef: "start", TargetRef: "check"}},
	}
}

// finishTheOnlyStep completes the step of a nothingFollows instance, checks
// that it left the instance active and holding no token, and returns the task.
func finishTheOnlyStep(t *testing.T, h engineHarness, id uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := h.Ctx()
	open := openIterationTasks(ctx, t, h, id, "check")
	if len(open) != 1 {
		t.Fatalf("the step has %d open task(s), want one", len(open))
	}
	if err := h.svc.CompleteTask(ctx, open[0].ID, "rita", nil); err != nil {
		t.Fatalf("complete the step: %v", err)
	}
	if left := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive); len(left.Tokens) != 0 {
		t.Fatalf("the instance still holds %d token(s); this fixture needs it to hold none", len(left.Tokens))
	}
	return open[0].ID
}

// Rulings addendum §10 and §13 (plan Rulings 21, 23, 24). An instance that is
// active and waits nowhere can be closed by a cancel that names no step, and
// by nothing else. The plan accepts it and says in a warning what it is
// closing — and, when a task is still open under it or a caller is still
// waiting for it, says that too.
func TestACancelThatNamesNoStepClosesAnInstanceWithNothingLeft(t *testing.T) {
	h := newEngineHarness(t, "Plan Nothing Left Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, nothingFollows(h.projID, "plan-nothing-left"), nil)
	finished := finishTheOnlyStep(t, h, id)

	nothingLeft := "This instance has nothing left to do: it is not waiting at any step, and nothing will move it on. Cancelling it closes it."
	closing := deviationCommand(entities.DeviationCancel, id, "", nil)
	plan := w.preview(t, closing)
	if !plan.Applicable() || plan.NodeID != "" || plan.NodeName != "" || plan.Scope != entities.DeviationScopeInstance ||
		len(plan.OpenWork) != 0 || len(plan.VisitKey) != 36 || !reflect.DeepEqual(plan.Warnings, []string{nothingLeft}) {
		t.Fatalf("the plan for an instance with nothing left: %+v\nrefusals:%s\nwarnings:%s", plan, lines(plan.Refusals), lines(plan.Warnings))
	}
	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationHold, entities.DeviationCancel} {
		if at := w.preview(t, deviationCommand(kind, id, "check", nil)); !said(at.Refusals, "This instance is not waiting at “Check the order”.") {
			t.Errorf("a %s at the step the instance has left:%s", kind, lines(at.Refusals))
		}
	}

	// A task open with no token under it: what a migration with a mapping left
	// in 0.4.0, by offering a finished task again (docs/upgrading.md, "A task
	// a migration reopened"). No release since creates one, so the row is put
	// back as that one left it, through the repository.
	if err := h.repo.Task().UpdateStatus(ctx, finished, models.TaskClaimed); err != nil {
		t.Fatalf("reopen the finished task: %v", err)
	}
	reopened := w.preview(t, closing)
	wantWarnings := []string{
		nothingLeft,
		"“Check the order” is still open though the instance is not waiting there; it will be withdrawn.",
		"“Check the order” is with rita, who will be told it was withdrawn.",
	}
	if !reopened.Applicable() || !reflect.DeepEqual(reopened.Warnings, wantWarnings) {
		t.Errorf("with a task still open under it: refusals:%s\nwarnings:%s\nwant the warnings:%s",
			lines(reopened.Refusals), lines(reopened.Warnings), lines(wantWarnings))
	}
	if len(reopened.OpenWork) != 1 || reopened.OpenWork[0].TaskID != finished {
		t.Errorf("the open work %+v, want the reopened task", reopened.OpenWork)
	}
	if reopened.VisitKey == plan.VisitKey {
		t.Error("a task opening on an instance that held nothing did not change the key of the cancel that closes it")
	}

	// A called instance with nothing left never ends, so it never resumes its
	// caller, and its caller cannot be cancelled while it is active.
	h.deploy(t, nothingFollows(h.projID, "plan-stranded-called"))
	caller := w.start(t, callerOf(h.projID, "plan-stranded-caller", "plan-stranded-called"), nil)
	called := theOneCalledBy(t, h, caller)
	finishTheOnlyStep(t, h, called)
	alone := w.preview(t, deviationCommand(entities.DeviationCancel, called, "", nil))
	wantWarnings = []string{
		nothingLeft,
		"This instance was started by another process (instance " + caller.String() + "), which is still waiting for it and is not resumed by this; cancel or hold that one next.",
	}
	if !alone.Applicable() || !reflect.DeepEqual(alone.Warnings, wantWarnings) {
		t.Errorf("closing a called instance with nothing left: refusals:%s\nwarnings:%s\nwant the warnings:%s",
			lines(alone.Refusals), lines(alone.Warnings), lines(wantWarnings))
	}
	if around := w.preview(t, deviationCommand(entities.DeviationCancel, caller, "haveItChecked", nil)); around.Applicable() {
		t.Error("the caller was cancellable around a called instance that is still active")
	}
}
