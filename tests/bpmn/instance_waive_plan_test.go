package bpmn_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
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
		OpenWork: []entities.DeviationOpenWork{{TaskID: task.ID, Name: "Operations approve", NodeID: "opsApprove", NodeName: "Operations approve",
			Status: entities.TaskClaimed, Assignee: "ollie"}},
		OpenWorkInAll: 1,
		Outputs:       map[string]any{"approved": true},
		// A waive is asked of a second administrator; the plan says so.
		RequiresSecondApprover: true,
		Warnings:               []string{"“Operations approve” is with ollie, who will be told it was withdrawn."},
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
	// Nor is a hand-over: the administrator gives the task to somebody else,
	// and the plan names them without becoming another visit.
	if err := h.svc.CreateUser(ctx, entities.User{
		Username: "sam", Roles: []string{entities.RoleOperator},
		Organizations: []*entities.Organization{{ID: entities.ActingOrganization(ctx)}},
	}, "hand-over-test-password"); err != nil {
		t.Fatalf("create sam: %v", err)
	}
	if err := h.svc.AssignTask(w.ctx, claimed.OpenWork[0].TaskID, servicecontracts.HandOver{
		Actor: "ana", Target: "sam", Reason: "Olga is away this week.",
	}); err != nil {
		t.Fatalf("hand the task to sam: %v", err)
	}
	handedOver := w.preview(t, sales)
	if handedOver.VisitKey != unclaimed.VisitKey || len(handedOver.OpenWork) != 1 || handedOver.OpenWork[0].Assignee != "sam" ||
		!said(handedOver.Warnings, "“Sales approve” is with sam, who will be told it was withdrawn.") {
		t.Errorf("after a hand-over: the key is the same %v, open work %+v, warnings:%s",
			handedOver.VisitKey == unclaimed.VisitKey, handedOver.OpenWork, lines(handedOver.Warnings))
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
			!reflect.DeepEqual(point.Missing, want.Missing) || point.Analysed != want.Analysed || point.ReadsInAll != len(want.Reads) {
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
	if !listed || called.Analysed || len(called.Missing) != 0 || !reflect.DeepEqual(called.Reads, []string{"reviewers", "urgent"}) ||
		plan.DecisionPointsInAll != len(plan.DecisionPoints) {
		t.Fatalf("the called process: %+v (listed %v)", called, listed)
	}
	// A step that repeats does not decide from the list: it takes its runs
	// from it.
	wantRefusal := "“Ask each reviewer” takes its list of runs from reviewers, which “Pick the reviewers” would have set; say what the waiver counts as by supplying reviewers."
	if !reflect.DeepEqual(plan.Refusals, []string{wantRefusal}) {
		t.Errorf("the refusals:%s\nwant only\n  %s", lines(plan.Refusals), wantRefusal)
	}
	wantWarning := "“Check the supplier” starts another process and hands it reviewers, urgent, which “Pick the reviewers” would have set; " +
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

// Design §7.4 rule 12, and the final-wave ruling FW-1 in place of rule 11. An
// instance waiting on one it started cannot be ended under it: the plan
// refuses, names the other instance, and lists what is still running. A called
// instance that is waiting can be cancelled, and the plan warns that its caller
// is still waiting for it and is not resumed.
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
	wantWarnings := []string{
		"This instance was started by another process (instance " + caller.String() + "), which is still waiting for it and is not resumed by this; cancel or hold that one next.",
		"“Review the supplier” is with rita, who will be told it was withdrawn.",
	}
	if !inside.Applicable() || !reflect.DeepEqual(inside.Warnings, wantWarnings) || len(inside.CalledInstances) != 0 {
		t.Errorf("cancelling a called instance: called %v, refusals:%s\nwarnings:%s\nwant it accepted with the warnings:%s",
			inside.CalledInstances, lines(inside.Refusals), lines(inside.Warnings), lines(wantWarnings))
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
	want := "This instance is waiting at “Check credit”, “Check stock”; say which of those steps it is to be ended at."
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
// waiting nowhere, reached the way production reaches it. Whether anything
// else could still move such an instance — a timer, a message — is not
// something these tests or the plan look at.
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
func TestACancelThatNamesNoStepClosesAnInstanceThatWaitsNowhere(t *testing.T) {
	h := newEngineHarness(t, "Plan Waits Nowhere Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, nothingFollows(h.projID, "plan-waits-nowhere"), nil)
	finished := finishTheOnlyStep(t, h, id)

	// Only what was looked at is said: where it waits. Whether a timer or a
	// message could still move it was not looked at, so it is not claimed.
	waitsNowhere := "This instance is not waiting at any step. Cancelling it closes it."
	closing := deviationCommand(entities.DeviationCancel, id, "", nil)
	plan := w.preview(t, closing)
	if !plan.Applicable() || plan.NodeID != "" || plan.NodeName != "" || plan.Scope != entities.DeviationScopeInstance ||
		len(plan.OpenWork) != 0 || len(plan.VisitKey) != 36 || !reflect.DeepEqual(plan.Warnings, []string{waitsNowhere}) {
		t.Fatalf("the plan for an instance that waits nowhere: %+v\nrefusals:%s\nwarnings:%s", plan, lines(plan.Refusals), lines(plan.Warnings))
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
		waitsNowhere,
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

	// A called instance that waits nowhere reaches no end event, so it does
	// not resume its caller, and its caller cannot be cancelled while it has
	// not ended.
	h.deploy(t, nothingFollows(h.projID, "plan-stranded-called"))
	caller := w.start(t, callerOf(h.projID, "plan-stranded-caller", "plan-stranded-called"), nil)
	called := theOneCalledBy(t, h, caller)
	finishTheOnlyStep(t, h, called)
	alone := w.preview(t, deviationCommand(entities.DeviationCancel, called, "", nil))
	wantWarnings = []string{
		waitsNowhere,
		"This instance was started by another process (instance " + caller.String() + "), which is still waiting for it and is not resumed by this; cancel or hold that one next.",
	}
	if !alone.Applicable() || !reflect.DeepEqual(alone.Warnings, wantWarnings) {
		t.Errorf("closing a called instance that waits nowhere: refusals:%s\nwarnings:%s\nwant the warnings:%s",
			lines(alone.Refusals), lines(alone.Warnings), lines(wantWarnings))
	}
	if around := w.preview(t, deviationCommand(entities.DeviationCancel, caller, "haveItChecked", nil)); around.Applicable() {
		t.Error("the caller was cancellable around a called instance that is still active")
	}
}

// A cancel ends the whole instance, whichever step it names: it withdraws the
// work on every branch. So the plan shows all of it, warns everybody who
// would lose work, and its key is of the whole instance — the other branch
// moving on between the preview and the apply is work the preview did not
// show, and the preview goes stale.
func TestACancelShowsAndKeysEverythingItWouldWithdraw(t *testing.T) {
	h := newEngineHarness(t, "Plan Cancel Everything Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-cancel-branches",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "stock", Type: entities.UserTask, Name: "Check stock", Assignee: "sam"},
			{ID: "credit", Type: entities.UserTask, Name: "Check credit"},
			{ID: "limit", Type: entities.UserTask, Name: "Set the credit limit", Assignee: "cara"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "stock"},
			{ID: "f3", SourceRef: "fork", TargetRef: "credit"},
			{ID: "f4", SourceRef: "credit", TargetRef: "limit"},
			{ID: "f5", SourceRef: "stock", TargetRef: "join"},
			{ID: "f6", SourceRef: "limit", TargetRef: "join"},
			{ID: "f7", SourceRef: "join", TargetRef: "end"},
		},
	}, nil)
	steps := func(plan entities.DeviationPlan) []string {
		var on []string
		for _, work := range plan.OpenWork {
			on = append(on, work.NodeID+" “"+work.NodeName+"” "+work.Assignee)
		}
		slices.Sort(on)
		return on
	}
	cancel := deviationCommand(entities.DeviationCancel, id, "stock", nil)
	hold := deviationCommand(entities.DeviationHold, id, "stock", nil)
	waive := deviationCommand(entities.DeviationWaive, id, "stock", nil)

	before := w.preview(t, cancel)
	if want := []string{"credit “Check credit” ", "stock “Check stock” sam"}; !before.Applicable() || !reflect.DeepEqual(steps(before), want) {
		t.Fatalf("a cancel at one of two branches lists %v, want the work of both: %v\nrefusals:%s", steps(before), want, lines(before.Refusals))
	}
	if want := []string{"“Check stock” is with sam, who will be told it was withdrawn."}; !reflect.DeepEqual(before.Warnings, want) {
		t.Errorf("the warnings:%s\nwant:%s", lines(before.Warnings), lines(want))
	}
	holdBefore, waiveBefore := w.preview(t, hold), w.preview(t, waive)
	if want := []string{"stock “Check stock” sam"}; !reflect.DeepEqual(steps(holdBefore), want) || !reflect.DeepEqual(steps(waiveBefore), want) {
		t.Errorf("a hold lists %v and a waive %v; each acts on the step alone: %v", steps(holdBefore), steps(waiveBefore), want)
	}

	// The other branch moves on, to work somebody holds.
	completeTaskAt(ctx, t, h, id, "credit", nil)
	after := w.preview(t, cancel)
	if want := []string{"limit “Set the credit limit” cara", "stock “Check stock” sam"}; !reflect.DeepEqual(steps(after), want) {
		t.Fatalf("after the other branch moved on the cancel lists %v, want %v", steps(after), want)
	}
	wantWarnings := []string{
		"“Check stock” is with sam, who will be told it was withdrawn.",
		"“Set the credit limit” is with cara, who will be told it was withdrawn.",
	}
	slices.Sort(after.Warnings)
	if !reflect.DeepEqual(after.Warnings, wantWarnings) {
		t.Errorf("the warnings:%s\nwant:%s", lines(after.Warnings), lines(wantWarnings))
	}
	if after.VisitKey == before.VisitKey {
		t.Error("the other branch moved on and the cancel's key did not change: a preview that no longer shows what would be withdrawn must go stale")
	}
	// A hold and a waive of this step are the same work as before.
	if w.preview(t, hold).VisitKey != holdBefore.VisitKey || w.preview(t, waive).VisitKey != waiveBefore.VisitKey {
		t.Error("the other branch moving on changed the key of a hold or a waive of this step")
	}
}

// twoFlowsIntoOneStep is start → fork → Check the order, entered by both of
// the fork's flows → Pack the order → end.
func twoFlowsIntoOneStep(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "check", Type: entities.UserTask, Name: "Check the order", Assignee: "rita"},
			{ID: "pack", Type: entities.UserTask, Name: "Pack the order", Assignee: "paul"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "check"},
			{ID: "f3", SourceRef: "fork", TargetRef: "check"},
			{ID: "f4", SourceRef: "check", TargetRef: "pack"},
			{ID: "f5", SourceRef: "pack", TargetRef: "end"},
		},
	}
}

// tokensOn counts the tokens an instance holds on a step.
func tokensOn(t *testing.T, h engineHarness, id uuid.UUID, nodeID string) int {
	t.Helper()
	instance, err := h.engine.GetInstance(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	return len(instance.GetTokensByNode(&entities.Node{ID: nodeID}))
}

// BPMN 2.0.2 §10.5.4 (Parallel Gateway): each outgoing flow of a fork gets a
// token, so a step both flows enter is reached twice, with a token and a task
// for each. Done by hand, each task finished sends the instance on once. A
// waive ends the step whole and moves on once, so it would drop one of the
// two without a word: it is refused, and a hold and a cancel are not.
func TestAWaiveOfAStepReachedTwiceAtOnceIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Plan Reached Twice Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, twoFlowsIntoOneStep(h.projID, "plan-reached-twice"), nil)
	open := openIterationTasks(ctx, t, h, id, "check")
	if tokens := tokensOn(t, h, id, "check"); tokens != 2 || len(open) != 2 {
		t.Fatalf("the step holds %d token(s) and %d open task(s); this test needs the engine to have reached it twice", tokens, len(open))
	}

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "check", nil))
	want := "“Check the order” was reached 2 times at once on this instance, and a waive would move the instance on only once. " +
		"Complete or reassign its tasks instead, or hold the instance."
	if !reflect.DeepEqual(plan.Refusals, []string{want}) {
		t.Fatalf("the refusals:%s\nwant only\n  %s", lines(plan.Refusals), want)
	}
	for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationCancel} {
		if other := w.preview(t, deviationCommand(kind, id, "check", nil)); !other.Applicable() || len(other.OpenWork) != 2 {
			t.Errorf("a %s of a step reached twice: %d open, refusals:%s", kind, len(other.OpenWork), lines(other.Refusals))
		}
	}

	// What the refusal says of doing it by hand is what the engine does: both
	// tasks finished, the instance is at the next step twice.
	for _, task := range open {
		if err := h.svc.CompleteTask(ctx, task.ID, "rita", nil); err != nil {
			t.Fatalf("complete %s: %v", task.ID, err)
		}
	}
	if tokens := tokensOn(t, h, id, "pack"); tokens != 2 {
		t.Fatalf("after both tasks were finished by hand the next step holds %d token(s), want 2", tokens)
	}
	// A repeating approval holds a token for each run on purpose, and is
	// waived whole.
	approval := startApproval(t, h, approvalDefinition(h.projID, "plan-reached-runs", "parallel", ""), "ana", "budi", "citra")
	if runs := w.preview(t, deviationCommand(entities.DeviationWaive, approval, "approve", nil)); !runs.Applicable() {
		t.Errorf("a repeating approval with three runs open was refused:%s", lines(runs.Refusals))
	}
}

// A cancel withdraws work the process is no longer waiting for as well as the
// work it is, and the plan says which is which. Reached the way production
// reaches it: a step entered twice at once gives up both its tokens when the
// first of its two tasks is finished, and the second task stays open under an
// instance that has moved on.
func TestACancelSaysWhichOpenWorkTheInstanceIsNoLongerWaitingFor(t *testing.T) {
	h := newEngineHarness(t, "Plan Left Behind Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, twoFlowsIntoOneStep(h.projID, "plan-left-behind"), nil)
	open := openIterationTasks(ctx, t, h, id, "check")
	if err := h.svc.CompleteTask(ctx, open[0].ID, "rita", nil); err != nil {
		t.Fatalf("complete one of the two: %v", err)
	}
	if on, left := tokensOn(t, h, id, "check"), len(openIterationTasks(ctx, t, h, id, "check")); on != 0 || left != 1 {
		t.Fatalf("after one completion the step holds %d token(s) and %d open task(s); this test needs none and one", on, left)
	}

	plan := w.preview(t, deviationCommand(entities.DeviationCancel, id, "pack", nil))
	want := []string{
		"“Check the order” is still open though the instance is not waiting there; it will be withdrawn.",
		"“Check the order” is with rita, who will be told it was withdrawn.",
		"“Pack the order” is with paul, who will be told it was withdrawn.",
	}
	if !plan.Applicable() || len(plan.OpenWork) != 2 || !reflect.DeepEqual(plan.Warnings, want) {
		t.Errorf("%d open, refusals:%s\nwarnings:%s\nwant the warnings:%s", len(plan.OpenWork), lines(plan.Refusals), lines(plan.Warnings), lines(want))
	}
	// A hold withdraws nothing, and says nothing of it.
	if hold := w.preview(t, deviationCommand(entities.DeviationHold, id, "pack", nil)); len(hold.Warnings) != 0 || len(hold.OpenWork) != 1 {
		t.Errorf("a hold: %d open, warnings:%s", len(hold.OpenWork), lines(hold.Warnings))
	}
}

// suspend puts an instance in the suspended state. Nothing in the product
// suspends one today, so the row is written through the repository: the state
// is one the store keeps, an instance in it can be made active again, and so
// it has not ended.
func suspend(t *testing.T, h engineHarness, id uuid.UUID) {
	t.Helper()
	row, err := h.repo.Process().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read instance %s: %v", id, err)
	}
	row.Status = models.ProcessSuspended
	if err := h.repo.Process().Update(h.Ctx(), row); err != nil {
		t.Fatalf("suspend instance %s: %v", id, err)
	}
}

// An instance that is suspended has not ended: it can run again. So a called
// instance that is suspended still stops its caller being cancelled, and a
// caller that is suspended is still waiting for the instance it called.
func TestACalledInstanceOrACallerThatIsSuspendedHasNotEnded(t *testing.T) {
	h := newEngineHarness(t, "Plan Suspended Project")
	w := newWaiver(h)

	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-suspended-called",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "review"}, {ID: "c2", SourceRef: "review", TargetRef: "end"}},
	})
	caller := w.start(t, callerOf(h.projID, "plan-suspended-caller", "plan-suspended-called"), nil)
	called := theOneCalledBy(t, h, caller)
	suspend(t, h, called)
	around := w.preview(t, deviationCommand(entities.DeviationCancel, caller, "haveItChecked", nil))
	want := "This instance is waiting on 1 process(es) it started (" + called.String() + "); cancel or finish those first."
	if !reflect.DeepEqual(around.Refusals, []string{want}) || !reflect.DeepEqual(around.CalledInstances, []uuid.UUID{called}) {
		t.Errorf("cancelling around a suspended called instance: called %v, refusals:%s\nwant only\n  %s", around.CalledInstances, lines(around.Refusals), want)
	}
	waive := w.preview(t, deviationCommand(entities.DeviationWaive, caller, "haveItChecked", nil))
	if want := "“Have it checked” runs another process; waive the step inside that process (instance " + called.String() + ") instead."; !said(waive.Refusals, want) {
		t.Errorf("waiving a step whose called instance is suspended:%s\nwant\n  %s", lines(waive.Refusals), want)
	}

	// The other way round: the caller is suspended, and the instance it
	// called waits nowhere.
	h.deploy(t, nothingFollows(h.projID, "plan-suspended-stranded"))
	waiting := w.start(t, callerOf(h.projID, "plan-suspended-waiting", "plan-suspended-stranded"), nil)
	stranded := theOneCalledBy(t, h, waiting)
	finishTheOnlyStep(t, h, stranded)
	suspend(t, h, waiting)
	alone := w.preview(t, deviationCommand(entities.DeviationCancel, stranded, "", nil))
	wantWarning := "This instance was started by another process (instance " + waiting.String() + "), which is still waiting for it and is not resumed by this; cancel or hold that one next."
	if !alone.Applicable() || !said(alone.Warnings, wantWarning) {
		t.Errorf("closing an instance whose caller is suspended: refusals:%s\nwarnings:%s\nwant the warning\n  %s", lines(alone.Refusals), lines(alone.Warnings), wantWarning)
	}
}

// watchedForms is the stored forms, counting how often one is read and
// failing when told to.
type watchedForms struct {
	repocontracts.FormRepository
	reads int
	fail  error
}

func (f *watchedForms) GetByKey(ctx context.Context, projectID uuid.UUID, key string) (models.FormModel, error) {
	f.reads++
	if f.fail != nil {
		return models.FormModel{}, f.fail
	}
	return f.FormRepository.GetByKey(ctx, projectID, key)
}

// repositoryWatchingForms is a repository whose stored forms are watched.
type repositoryWatchingForms struct {
	repositories.Repository
	forms *watchedForms
}

func (r repositoryWatchingForms) Form() repocontracts.FormRepository { return r.forms }

// Plan Ruling 8. What a waive may set is what every open run of the step
// could have set. Runs of one step carry one form, so the forms differ here
// only because one run's row is rewritten through the repository — a state no
// release creates today, and one the rule must still hold in.
//
// The stored form the runs name is read once for all of them, and a form that
// cannot be read is an error: it is never taken to declare nothing.
func TestAWaiveMaySetOnlyWhatEveryOpenRunsFormDeclares(t *testing.T) {
	h := newEngineHarness(t, "Plan Forms Project")
	ctx := h.Ctx()
	if err := h.repo.Form().Create(ctx, models.FormModel{
		Base: models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))}, ProjectID: models.FromUUID(h.projID),
		Key: "purchase-approval", Name: "Purchase approval",
		Schema: map[string]any{"fields": []any{map[string]any{"id": "comment", "label": "Comment", "type": "text"}}},
	}); err != nil {
		t.Fatalf("store the form: %v", err)
	}
	def := approvalDefinition(h.projID, "plan-forms", "parallel", "")
	def.Nodes[1].FormKey = "purchase-approval"
	id := startApproval(t, h, def, "ana", "budi", "citra")

	forms := &watchedForms{FormRepository: h.repo.Form()}
	w := newWaiver(h)
	w.svc = serviceimpl.NewInstanceDeviationService(repositoryWatchingForms{Repository: h.repo, forms: forms}, h.engine)

	// Every run declares decision itself and comment through the stored form.
	same := w.preview(t, deviationCommand(entities.DeviationWaive, id, "approve", map[string]any{"decision": "yes", "comment": "waived"}))
	if !same.Applicable() || len(same.OpenWork) != 3 {
		t.Fatalf("three runs with one form: %d open, refusals:%s", len(same.OpenWork), lines(same.Refusals))
	}
	if forms.reads != 1 {
		t.Errorf("the stored form was read %d times for three runs that name it, want once", forms.reads)
	}

	// One run's form now declares amount as well.
	open := openIterationTasks(ctx, t, h, id, "approve")
	row, err := h.repo.Task().Get(ctx, open[0].ID)
	if err != nil {
		t.Fatalf("read a run's task: %v", err)
	}
	row.FormDefinition = `[{"id":"decision","label":"Decision","type":"text"},{"id":"amount","label":"Amount","type":"number"}]`
	if err := h.repo.Task().Update(ctx, row); err != nil {
		t.Fatalf("rewrite a run's form: %v", err)
	}
	some := w.preview(t, deviationCommand(entities.DeviationWaive, id, "approve", map[string]any{"decision": "yes", "amount": 900}))
	want := "amount is not declared by every open task of “Approve the purchase”, so a waiver cannot supply it."
	if !reflect.DeepEqual(some.Refusals, []string{want}) {
		t.Errorf("a value only one run's form declares:%s\nwant only\n  %s", lines(some.Refusals), want)
	}
	if every := w.preview(t, deviationCommand(entities.DeviationWaive, id, "approve", map[string]any{"decision": "yes"})); !every.Applicable() {
		t.Errorf("a value every run's form declares was refused:%s", lines(every.Refusals))
	}

	// A form that cannot be read fails the plan.
	forms.fail = errors.New("the forms cannot be read just now")
	command := deviationCommand(entities.DeviationWaive, id, "approve", map[string]any{"decision": "yes"})
	command.DryRun = true
	if out, err := w.svc.DeviateInstance(w.ctx, command); !errors.Is(err, forms.fail) {
		t.Errorf("with the stored form unreadable: %v, plan %+v; want the read's error and no plan", err, out.Plan)
	}
}

// engineThatLostTheVersion is the engine answering that the version an
// instance runs is not there.
type engineThatLostTheVersion struct {
	servicecontracts.ExecutionEngine
}

func (engineThatLostTheVersion) GetProcessDefinition(context.Context, uuid.UUID) (*entities.ProcessDefinition, error) {
	return nil, fmt.Errorf("%w: no such process definition", apierr.ErrNotFound)
}

// An administrator who may read the instance asked about an instance that is
// there. If the version it runs cannot be found, that is the server's trouble
// and is answered as that — not as "not found", which over the route is a 404
// and says the instance does not exist.
func TestAnInstanceWhoseVersionIsGoneIsNotAnsweredAsNoSuchInstance(t *testing.T) {
	h := newEngineHarness(t, "Plan Lost Version Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "plan-lost-version"), nil)
	w.svc = serviceimpl.NewInstanceDeviationService(h.repo, engineThatLostTheVersion{h.engine})

	command := deviationCommand(entities.DeviationHold, id, "opsApprove", nil)
	command.DryRun = true
	_, err := w.svc.DeviateInstance(w.ctx, command)
	if err == nil || errors.Is(err, apierr.ErrNotFound) || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("got %v; want an error that is none of the caller's doing", err)
	}
	if !strings.Contains(err.Error(), "is not there") {
		t.Errorf("the error %q does not say what is missing", err)
	}
}

// A plan reads no more than 64 decision tables, whatever the process
// consults. A step whose decision was left unread for that is told so in
// words — reading it again would not help — and the steps a plan does not
// name one by one are counted.
func TestAPlanSaysWhichDecisionsItDidNotReadForTheirNumber(t *testing.T) {
	h := newEngineHarness(t, "Plan Many Decisions Project")
	w := newWaiver(h)
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-many-decisions",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved")},
		},
		Flows: []*entities.SequenceFlow{{ID: "f0", SourceRef: "start", TargetRef: "review"}},
	}
	// Seventy steps, each consulting a decision of its own that nobody
	// stored. The first sixty-four are looked for; the last six are listed
	// first, by their ids, so that what is said of them is seen.
	previous := "review"
	for i := range 70 {
		id := fmt.Sprintf("z-looked-for-%02d", i)
		if i >= 64 {
			id = fmt.Sprintf("a-past-the-bound-%02d", i)
		}
		def.Nodes = append(def.Nodes, &entities.Node{ID: id, Type: entities.BusinessRuleTask, Name: fmt.Sprintf("Rule %02d", i),
			Properties: map[string]any{"decision_key": fmt.Sprintf("policy-%02d", i)}})
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: fmt.Sprintf("f%02d", i+1), SourceRef: previous, TargetRef: id})
		previous = id
	}
	def.Nodes = append(def.Nodes, &entities.Node{ID: "end", Type: entities.EndEvent})
	def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "f-end", SourceRef: previous, TargetRef: "end"})
	id := w.start(t, def, nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	if !plan.Applicable() || plan.DecisionPointsInAll != 70 || len(plan.DecisionPoints) != 70 {
		t.Fatalf("%d decision points listed of %d, refusals:%s", len(plan.DecisionPoints), plan.DecisionPointsInAll, lines(plan.Refusals))
	}
	want := []string{"“Review the claim” is with rita, who will be told it was withdrawn."}
	for i := 64; i < 70; i++ {
		want = append(want, fmt.Sprintf("“Rule %02d” was not read: this process consults more decision tables than one preview reads (64). Check it before applying.", i))
	}
	for i := range 4 {
		want = append(want, fmt.Sprintf("“Rule %02d” could not be read to see what it decides from; check it before applying.", i))
	}
	want = append(want, "60 more steps could not be read either; check them before applying.")
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Errorf("the warnings:%s\nwant:%s", lines(plan.Warnings), lines(want))
	}
}

// A decision point names the first ten values it is missing. The plan's own
// list names them all, so that nothing that stops a waive has to be found by
// previewing again; and when there are more than one waive may set, the plan
// says so at once.
func TestThePlanNamesEveryMissingValueAndSaysWhenOneWaiveCannotSetThemAll(t *testing.T) {
	h := newEngineHarness(t, "Plan Missing Project")
	w := newWaiver(h)
	process := func(key string, fields []string) *entities.ProcessDefinition {
		return &entities.ProcessDefinition{
			Project: &entities.Project{ID: h.projID}, Key: key,
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "fill", Type: entities.UserTask, Name: "Fill in the claim", Assignee: "rita", Properties: testutils.FormDeclaring(fields...)},
				{ID: "complete", Type: entities.ExclusiveGateway, Name: "Complete?", DefaultFlow: "no"},
				{ID: "pay", Type: entities.UserTask, Name: "Pay the claim"},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "fill"},
				{ID: "f2", SourceRef: "fill", TargetRef: "complete"},
				{ID: "yes", SourceRef: "complete", TargetRef: "pay", Condition: strings.Join(fields, " and ")},
				{ID: "no", SourceRef: "complete", TargetRef: "end"},
				{ID: "f3", SourceRef: "pay", TargetRef: "end"},
			},
		}
	}
	numbered := func(count int) []string {
		fields := make([]string, count)
		for i := range fields {
			fields[i] = fmt.Sprintf("part%02d", i)
		}
		return fields
	}

	twelve := numbered(12)
	id := w.start(t, process("plan-missing-twelve", twelve), nil)
	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "fill", nil))
	point, listed := pointOfKind(plan, "complete", entities.DecisionPointGateway)
	if !listed || !reflect.DeepEqual(point.Missing, twelve[:10]) || point.MissingInAll != 12 {
		t.Fatalf("the gateway names %v of %d missing (listed %v); want the first ten of twelve", point.Missing, point.MissingInAll, listed)
	}
	if !reflect.DeepEqual(plan.Missing, twelve) || plan.MissingInAll != 12 {
		t.Errorf("the plan names %v of %d missing; want all twelve", plan.Missing, plan.MissingInAll)
	}
	want := "“Complete?” decides from part00, part01, part02, part03, part04, part05, part06, part07, part08, part09 and 2 more, which “Fill in the claim” would have set; " +
		"say what the waiver counts as by supplying part00, part01, part02, part03, part04, part05, part06, part07, part08, part09 and 2 more."
	if !reflect.DeepEqual(plan.Refusals, []string{want}) {
		t.Errorf("the refusals:%s\nwant only\n  %s", lines(plan.Refusals), want)
	}
	// What the plan named is enough: given all twelve, nothing refuses.
	given := map[string]any{}
	for _, name := range plan.Missing {
		given[name] = true
	}
	if all := w.preview(t, deviationCommand(entities.DeviationWaive, id, "fill", given)); !all.Applicable() || len(all.Missing) != 0 || all.MissingInAll != 0 {
		t.Errorf("with every value the plan named given: %d still missing, refusals:%s", all.MissingInAll, lines(all.Refusals))
	}

	sixty := numbered(60)
	id = w.start(t, process("plan-missing-sixty", sixty), nil)
	plan = w.preview(t, deviationCommand(entities.DeviationWaive, id, "fill", nil))
	if !reflect.DeepEqual(plan.Missing, sixty[:entities.MaxDeviationOutputs]) || plan.MissingInAll != 60 {
		t.Errorf("the plan names %d of %d missing; want the first %d of sixty", len(plan.Missing), plan.MissingInAll, entities.MaxDeviationOutputs)
	}
	want = "This process decides from 60 values “Fill in the claim” would have set, and one waive may set at most 50. " +
		"Complete or reassign “Fill in the claim” instead, or hold the instance."
	if !said(plan.Refusals, want) {
		t.Errorf("the plan does not refuse with\n  %s\nits refusals:%s", want, lines(plan.Refusals))
	}
}

// A process is somebody's input, so a plan lists no more than a hundred of
// its decision points: those missing a value first, then those nobody could
// read, then the rest — each group in the order of its steps' ids — and says
// how many there are and how many it left out. The refusals still come from
// all of them.
func TestAPlanListsAHundredDecisionPointsTheOnesToActOnFirst(t *testing.T) {
	h := newEngineHarness(t, "Plan Hundred Points Project")
	w := newWaiver(h)
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "plan-hundred-points",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved", "amount")},
		},
		Flows: []*entities.SequenceFlow{{ID: "f0", SourceRef: "start", TargetRef: "review"}},
	}
	// 120 gateways in a row. Their ids are chosen so that the order of the
	// ids is the opposite of the order a plan lists them in: the eighty the
	// waive supplies come first by id, then the twenty nobody can read, then
	// the twenty missing a value.
	previous := "review"
	gateway := func(id, name, condition string) {
		def.Nodes = append(def.Nodes, &entities.Node{ID: id, Type: entities.ExclusiveGateway, Name: name, DefaultFlow: id + "-else"})
		def.Flows = append(def.Flows,
			&entities.SequenceFlow{ID: id + "-in", SourceRef: previous, TargetRef: id},
			&entities.SequenceFlow{ID: id + "-if", SourceRef: id, TargetRef: "end", Condition: condition},
		)
		previous = id
	}
	for i := range 80 {
		gateway(fmt.Sprintf("a-supplied-%02d", i), fmt.Sprintf("Supplied %02d", i), "approved")
	}
	for i := range 20 {
		gateway(fmt.Sprintf("m-unread-%02d", i), fmt.Sprintf("Unread %02d", i), "js:total > 10")
	}
	for i := range 20 {
		gateway(fmt.Sprintf("z-missing-%02d", i), fmt.Sprintf("Missing %02d", i), "amount > 100")
	}
	def.Nodes = append(def.Nodes, &entities.Node{ID: "end", Type: entities.EndEvent})
	for _, node := range def.Nodes {
		if node.Type == entities.ExclusiveGateway {
			def.Flows = append(def.Flows, &entities.SequenceFlow{ID: node.ID + "-else", SourceRef: node.ID, TargetRef: "end"})
		}
	}
	def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "last", SourceRef: previous, TargetRef: "end"})
	id := w.start(t, def, nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	if len(plan.DecisionPoints) != 100 || plan.DecisionPointsInAll != 120 {
		t.Fatalf("%d decision points listed of %d, want 100 of 120", len(plan.DecisionPoints), plan.DecisionPointsInAll)
	}
	var want []string
	for i := range 20 {
		want = append(want, fmt.Sprintf("z-missing-%02d", i))
	}
	for i := range 20 {
		want = append(want, fmt.Sprintf("m-unread-%02d", i))
	}
	for i := range 60 {
		want = append(want, fmt.Sprintf("a-supplied-%02d", i))
	}
	var got []string
	for _, point := range plan.DecisionPoints {
		got = append(got, point.NodeID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the points are listed in the order\n  %v\nwant those missing a value, then those nobody could read, then the rest:\n  %v", got, want)
	}
	if notListed := "20 more steps read what “Review the claim” would have set and are not listed here."; !said(plan.Warnings, notListed) {
		t.Errorf("the plan does not warn\n  %s\nits warnings:%s", notListed, lines(plan.Warnings))
	}
	// Every one of the twenty missing a value is refused for: ten by name,
	// ten counted.
	if len(plan.Refusals) != 11 || plan.Refusals[0] != "“Missing 00” decides from amount, which “Review the claim” would have set; say what the waiver counts as by supplying amount." ||
		plan.Refusals[10] != "10 more steps read values “Review the claim” would have set. In all, say what the waiver counts as by supplying amount." {
		t.Errorf("the refusals:%s", lines(plan.Refusals))
	}
	if !reflect.DeepEqual(plan.Missing, []string{"amount"}) || plan.MissingInAll != 1 {
		t.Errorf("the plan names %v of %d missing, want amount", plan.Missing, plan.MissingInAll)
	}
}

// A step done once for each of many people has a task for each, and a cancel
// lists the tasks of the whole instance. The plan shows two hundred and counts
// them all; its key, like the act, is of every one of them.
func TestAPlanListsTwoHundredOpenTasksAndKeysThemAll(t *testing.T) {
	h := newEngineHarness(t, "Plan Many Tasks Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	approvers := make([]any, 205)
	for i := range approvers {
		approvers[i] = fmt.Sprintf("approver%03d", i)
	}
	id := startApproval(t, h, approvalDefinition(h.projID, "plan-many-tasks", "parallel", ""), approvers...)
	open := openIterationTasks(ctx, t, h, id, "approve")
	if len(open) != 205 {
		t.Fatalf("the step has %d open tasks; this test needs 205", len(open))
	}

	for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationWaive, entities.DeviationHold} {
		plan := w.preview(t, deviationCommand(kind, id, "approve", nil))
		if len(plan.OpenWork) != 200 || plan.OpenWorkInAll != 205 || !plan.Applicable() {
			t.Errorf("a %s: %d tasks listed of %d, refusals:%s\nwant 200 of 205 and no refusal", kind, len(plan.OpenWork), plan.OpenWorkInAll, lines(plan.Refusals))
		}
		if notListed := "5 more tasks are open and are not listed here."; !reflect.DeepEqual(plan.Warnings, []string{notListed}) {
			t.Errorf("a %s warns:%s\nwant only\n  %s", kind, lines(plan.Warnings), notListed)
		}
	}

	// A task the plan does not list is finished: the work is different, and
	// the key with it. (Its run's token goes with it, which would change the
	// key by itself; that the key is made from the tasks not listed too is
	// pinned where no token moves, in the planner's own tests.)
	cancel := deviationCommand(entities.DeviationCancel, id, "approve", nil)
	before := w.preview(t, cancel)
	listed := map[uuid.UUID]bool{}
	for _, work := range before.OpenWork {
		listed[work.TaskID] = true
	}
	var unlisted *entities.Task
	for i := range open {
		if !listed[open[i].ID] {
			unlisted = &open[i]
			break
		}
	}
	if unlisted == nil {
		t.Fatal("every open task is listed; this test needs one that is not")
	}
	if err := completeAs(ctx, h, *unlisted, "carol", map[string]any{"decision": "yes"}); err != nil {
		t.Fatalf("complete a task the plan did not list: %v", err)
	}
	after := w.preview(t, cancel)
	if after.OpenWorkInAll != 204 || after.VisitKey == before.VisitKey {
		t.Errorf("after a task the plan did not list was finished: %d open in all, the key changed: %v; want 204 and a different key",
			after.OpenWorkInAll, after.VisitKey != before.VisitKey)
	}
}

// A step that calls a process once for each line of an order starts as many
// instances as the order has lines, and a cancel of their caller is refused
// while any has not ended. The plan is read by a script and by a screen and
// has a size whatever the order: it lists the two hundred with the lowest ids
// and counts them all, and the refusal counts every one.
func TestAPlanListsTwoHundredCalledInstancesAndCountsThemAll(t *testing.T) {
	h := newEngineHarness(t, "Plan Many Called Project")
	w := newWaiver(h)
	caller := h.startWaiting(t, "calls-many")
	const inAll, listed = 205, 200
	// As many called instances as a repeating call of that many lines starts:
	// recorded in one statement, as the listing's own test of scale does.
	h.seedInstances(t, caller, &caller, inAll)

	plan := w.preview(t, deviationCommand(entities.DeviationCancel, caller, "review", nil))
	if len(plan.CalledInstances) != listed || plan.CalledInstancesInAll != inAll {
		t.Fatalf("the plan lists %d called instances and counts %d; want %d of %d", len(plan.CalledInstances), plan.CalledInstancesInAll, listed, inAll)
	}
	if !slices.IsSortedFunc(plan.CalledInstances, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }) {
		t.Error("the called instances are not listed in the order of their ids")
	}
	every, err := h.engine.ListSubProcesses(h.Ctx(), caller)
	if err != nil || len(every) != inAll {
		t.Fatalf("the caller's called instances: %d (err %v)", len(every), err)
	}
	slices.SortFunc(every, func(a, b entities.ProcessInstance) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	if plan.CalledInstances[0] != every[0].ID || plan.CalledInstances[listed-1] != every[listed-1].ID {
		t.Error("the called instances listed are not the two hundred with the lowest ids")
	}
	var refusal string
	for _, said := range plan.Refusals {
		if strings.HasPrefix(said, "This instance is waiting on ") {
			refusal = said
		}
	}
	if !strings.HasPrefix(refusal, "This instance is waiting on 205 process(es) it started ("+every[0].ID.String()+", ") ||
		!strings.HasSuffix(refusal, " and 195 more); cancel or finish those first.") {
		t.Errorf("the refusal reads\n  %s\nwant it to count all 205, name the first ten and count the other 195", refusal)
	}
}
