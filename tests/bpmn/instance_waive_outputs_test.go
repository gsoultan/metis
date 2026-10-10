package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// variablesOf is the values an instance holds.
func variablesOf(t *testing.T, h engineHarness, id uuid.UUID) map[string]any {
	t.Helper()
	instance, err := h.engine.GetInstance(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	return instance.Variables
}

// AGENTS.md §0: no silent default at a decision point. A gateway reading
// approved after a waived review must not fall to an incident or a default:
// the administrator says what the waiver counts as, the instance is routed on
// that, and the record keeps it.
func TestAWaiveMustSayWhatItCountsAsForTheGatewayAfterIt(t *testing.T) {
	h := newEngineHarness(t, "Waive Outputs Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, claimWithAGateway(h.projID, "claim-outputs"), nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", nil))
	if _, listed := pointOfKind(plan, "decide", entities.DecisionPointGateway); plan.Applicable() || !listed ||
		!refusalMentions(plan, "“Approved?” decides from approved", "supplying approved") {
		t.Fatalf("the plan does not refuse for the gateway's missing approved: points %+v, refusals:%s", plan.DecisionPoints, lines(plan.Refusals))
	}
	_, err := w.apply(t, deviationCommand(entities.DeviationWaive, id, "review", nil))
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "supplying approved") {
		t.Fatalf("the apply answered %v, want it refused saying which value to supply", err)
	}
	if open := openIterationTasks(ctx, t, h, id, "review"); len(open) != 1 {
		t.Fatalf("a refused waive left %d open task(s) on the review, want the one", len(open))
	}

	out := w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": false}))
	if !h.waitingAt(ctx, t, id, "rework") || h.waitingAt(ctx, t, id, "pay") {
		t.Fatal("a waiver counted as not approved did not take the rework branch")
	}
	if got := variablesOf(t, h, id)["approved"]; got != false {
		t.Errorf("the instance holds approved = %v, want what the waiver counted as", got)
	}
	row := w.theWaive(t, id)
	after, _ := row.After["variables"].(map[string]any)
	if len(after) != 1 || after["approved"] != false {
		t.Errorf("the ledger's after.variables %v does not record what the waiver counted as", row.After["variables"])
	}
	// Nothing was there before, and the record does not make a value up.
	if was, said := row.Before["variables"].(map[string]any); said && len(was) != 0 {
		t.Errorf("the ledger's before.variables is %v for a value the instance did not hold", was)
	}
	// How many places read the step, as a count: the plan lists no more than a
	// screenful of them, and a list in the record would read as all of them.
	if row.Details["decision_points"] != float64(1) || row.Details["withdrawn"] != float64(1) {
		t.Errorf("the row's details %v, want one task withdrawn and one decision point", row.Details)
	}
	entries, err := h.svc.GetAuditLogs(ctx, id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type != serviceimpl.EventNodeSkipped {
			continue
		}
		if set, _ := entry.Data["outputs_set"].([]any); len(set) != 1 || set[0] != "approved" {
			t.Errorf("the entry says the waiver set %v, want approved", entry.Data["outputs_set"])
		}
		// The trail is not sealed; what was decided is in the ledger.
		for key, value := range entry.Data {
			if key == "approved" || value == false {
				t.Errorf("the entry carries a business value: %s = %v", key, value)
			}
		}
	}
	if out.Plan.Outputs["approved"] != false {
		t.Errorf("the outcome's plan does not say what was set: %v", out.Plan.Outputs)
	}
}

// loopingClaim is start → Review the claim (declares approved) → Approved? →
// pay → end, or → rework → back to the review.
func loopingClaim(projectID uuid.UUID, key string) *entities.ProcessDefinition {
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
			{ID: "back", SourceRef: "rework", TargetRef: "review"},
		},
	}
}

// Review Focus 3. On a second visit through a loop the instance already holds
// the previous visit's answer. Routing on it is the silent default this rule
// exists to stop, so a value the instance holds does not count: the preview
// refuses, the apply refuses, and the instance stays at the review until
// somebody says what this visit's waiver counts as.
func TestAValueLeftFromAnEarlierVisitDoesNotCountForTheGateway(t *testing.T) {
	h := newEngineHarness(t, "Waive Stale Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, loopingClaim(h.projID, "claim-stale"), nil)

	// The first visit: rita says no, the claim is reworked and comes back.
	first := theOpenTask(t, h, id, "review")
	if err := h.svc.CompleteTask(testutils.AsOperator(ctx, "rita"), first.ID, "rita", map[string]any{"approved": false}); err != nil {
		t.Fatalf("rita's first review: %v", err)
	}
	completeTaskAt(ctx, t, h, id, "rework", nil)
	if second := theOpenTask(t, h, id, "review"); second.ID == first.ID {
		t.Fatalf("the claim did not come back to the review: its open task is the first visit's, %+v", second)
	}
	if held := variablesOf(t, h, id)["approved"]; held != false {
		t.Fatalf("the instance holds approved = %v from the first visit, want false", held)
	}

	stale := deviationCommand(entities.DeviationWaive, id, "review", nil)
	if plan := w.preview(t, stale); plan.Applicable() || !refusalMentions(plan, "supplying approved") {
		t.Fatalf("a waive leaning on the value the first visit left: refusals:%s", lines(plan.Refusals))
	}
	if _, err := w.apply(t, stale); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the apply answered %v, want it refused", err)
	}
	// Routed on the stale false it would be at the rework again.
	if reworks := tasksEverOn(t, h, id, "rework"); reworks != 1 || !h.waitingAt(ctx, t, id, "review") {
		t.Fatalf("the refused waive moved the instance: %d rework task(s), waiting at the review %v",
			reworks, h.waitingAt(ctx, t, id, "review"))
	}
	if rows := w.ledger(t, id); len(rows) != 0 {
		t.Fatalf("a refused waive wrote %d ledger row(s)", len(rows))
	}

	// Said out loud, the second visit's waiver counts as approved, and the
	// record keeps what the value was before it.
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	if !h.waitingAt(ctx, t, id, "pay") || tasksEverOn(t, h, id, "rework") != 1 {
		t.Fatal("a waiver counted as approved did not take the pay branch")
	}
	row := w.theWaive(t, id)
	before, _ := row.Before["variables"].(map[string]any)
	after, _ := row.After["variables"].(map[string]any)
	if before["approved"] != false || after["approved"] != true {
		t.Errorf("the row says approved went from %v to %v, want false to true", before["approved"], after["approved"])
	}
}

// A waive is not a variable editor: it sets what a completion of the step
// could set, and nothing else — with the undeclared-variables escape hatch on
// as well, because that hatch is for a migration window, not for overrides.
func TestAWaiveSetsOnlyWhatTheStepsFormDeclares(t *testing.T) {
	t.Setenv(serviceimpl.EnvAllowUndeclaredTaskVariables, "true")
	h := newEngineHarness(t, "Waive Undeclared Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, claimWithAGateway(h.projID, "claim-undeclared"), nil)

	// The hatch is open: a completion would be let through with amount. This
	// is what makes the refusal below the waive's own.
	probe := w.start(t, claimWithAGateway(h.projID, "claim-undeclared-probe"), nil)
	probeTask := theOpenTask(t, h, probe, "review")
	if err := h.svc.CompleteTask(testutils.AsOperator(ctx, "rita"), probeTask.ID, "rita", map[string]any{"approved": true, "amount": 900}); err != nil {
		t.Fatalf("with the hatch open a completion setting amount was refused (%v), so the setting is not on and this proves nothing", err)
	}

	undeclared := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true, "amount": 900})
	plan := w.preview(t, undeclared)
	if plan.Applicable() || !refusalMentions(plan, "amount") {
		t.Fatalf("an undeclared output: refusals %q", plan.Refusals)
	}
	before := everyRow(t, h)
	_, err := w.apply(t, undeclared)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("applying a waive that sets what the form does not declare: %v, want it refused naming amount", err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused waive changed %v", changed)
	}
	if _, set := variablesOf(t, h, id)["amount"]; set {
		t.Fatal("the refused waive set amount")
	}

	// What the form declares is set, and only that.
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true}))
	held := variablesOf(t, h, id)
	if held["approved"] != true || len(held) != 1 {
		t.Fatalf("after the waive the instance holds %v, want approved and nothing else", held)
	}
}

// Every kind of decision point that reads what a waived step would have set
// is listed, wherever in the process it is: a gateway with a default flow, a
// gateway routed by a js: condition (listed as unread, a warning), a decision
// table's inputs, a condition a step waits for, a repeating step's list and
// its completion condition, and a process the instance calls.
func TestThePlanListsEveryKindOfDecisionPointThatReadsTheStep(t *testing.T) {
	h := newEngineHarness(t, "Waive Analysis Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	if _, err := h.svc.CreateDecision(ctx, entities.DecisionDefinition{
		ID: uuid.Must(uuid.NewV7()), Project: &entities.Project{ID: h.projID}, Key: "refundPolicy", Name: "Refund policy",
		Inputs:  []entities.DecisionInput{{ID: "in1", Label: "Approved", Expression: "approved", Type: "boolean"}},
		Outputs: []entities.DecisionOutput{{ID: "out1", Label: "Route", Name: "route", Type: "string"}},
		Rules:   []entities.DecisionRule{{ID: "r1", Inputs: []string{"true"}, Outputs: []any{"pay"}}},
	}); err != nil {
		t.Fatalf("deploy the decision: %v", err)
	}
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "claim-analysis-called",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "look", Type: entities.UserTask, Name: "Look the supplier up"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "look"}, {ID: "c2", SourceRef: "look", TargetRef: "end"}},
	})
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "claim-analysis",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita",
				Properties: testutils.FormDeclaring("approved", "reviewers")},
			{ID: "g1", Type: entities.ExclusiveGateway, Name: "Approved?", DefaultFlow: "g1-other"},
			{ID: "check", Type: entities.BusinessRuleTask, Name: "Refund policy", Properties: map[string]any{"decision_key": "refundPolicy"}},
			{ID: "g2", Type: entities.ExclusiveGateway, Name: "Route by script"},
			{ID: "work", Type: entities.UserTask, Name: "Do the work"},
			{ID: "watch", Type: entities.BoundaryEvent, Name: "Approval withdrawn", AttachedToRef: "work",
				Properties: map[string]any{"condition_expression": "approved"}},
			{ID: "ask", Type: entities.UserTask, Name: "Ask each reviewer", MultiInstanceType: "parallel",
				Collection: "reviewers", ElementVariable: "reviewer", CompletionCondition: "approved"},
			{ID: "supplier", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": "claim-analysis-called"}},
			{ID: "end", Type: entities.EndEvent},
			{ID: "stopped", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "g1"},
			{ID: "g1-yes", SourceRef: "g1", TargetRef: "check", Condition: "approved"},
			{ID: "g1-other", SourceRef: "g1", TargetRef: "check"},
			{ID: "f3", SourceRef: "check", TargetRef: "g2"},
			{ID: "g2-js", SourceRef: "g2", TargetRef: "work", Condition: "js:approved === true"},
			{ID: "g2-plain", SourceRef: "g2", TargetRef: "work", Condition: "approved = false"},
			{ID: "f4", SourceRef: "work", TargetRef: "ask"},
			{ID: "f5", SourceRef: "ask", TargetRef: "supplier"},
			{ID: "f6", SourceRef: "supplier", TargetRef: "end"},
			{ID: "w1", SourceRef: "watch", TargetRef: "stopped"},
		},
	}, nil)

	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review",
		map[string]any{"approved": true, "reviewers": []any{"rita", "sam"}}))
	if !plan.Applicable() {
		t.Fatalf("every read value is supplied and the plan refuses:%s", lines(plan.Refusals))
	}
	type at struct {
		node string
		kind entities.DecisionPointKind
	}
	listed := map[at]entities.DecisionPoint{}
	for _, point := range plan.DecisionPoints {
		listed[at{point.NodeID, point.Kind}] = point
	}
	want := []at{
		{"g1", entities.DecisionPointGateway},
		{"g2", entities.DecisionPointGateway},
		{"check", entities.DecisionPointDecisionTable},
		{"watch", entities.DecisionPointConditionalEvent},
		{"ask", entities.DecisionPointCollection},
		{"ask", entities.DecisionPointCompletionCondition},
		{"supplier", entities.DecisionPointCalledProcess},
	}
	for _, point := range want {
		if _, ok := listed[point]; !ok {
			t.Errorf("%s is not listed as a %s", point.node, point.kind)
		}
	}
	if len(plan.DecisionPoints) != len(want) || plan.DecisionPointsInAll != len(want) {
		t.Errorf("the plan lists %d decision points and counts %d, want %d: %+v",
			len(plan.DecisionPoints), plan.DecisionPointsInAll, len(want), plan.DecisionPoints)
	}
	kinds := map[entities.DecisionPointKind]bool{}
	for _, point := range plan.DecisionPoints {
		kinds[point.Kind] = true
	}
	if len(kinds) != 6 {
		t.Errorf("the plan lists %d kinds of decision point, want all six: %v", len(kinds), kinds)
	}
	if g1 := listed[at{"g1", entities.DecisionPointGateway}]; !g1.HasDefaultFlow || !g1.Analysed {
		t.Errorf("g1: %+v, want a default flow and analysed", g1)
	}
	if g2 := listed[at{"g2", entities.DecisionPointGateway}]; g2.Analysed {
		t.Errorf("a js: condition was claimed as read: %+v", g2)
	}
	if called := listed[at{"supplier", entities.DecisionPointCalledProcess}]; called.Analysed {
		t.Errorf("a called process nobody read was claimed as read: %+v", called)
	}
	for _, name := range []string{"Route by script", "Check the supplier"} {
		warned := false
		for _, warning := range plan.Warnings {
			warned = warned || strings.Contains(warning, name)
		}
		if !warned {
			t.Errorf("the warnings do not name “%s”, which nobody could read:%s", name, lines(plan.Warnings))
		}
	}

	// With nothing given, each point that could be read refuses for the
	// value it is missing; the two nobody could read refuse nothing.
	bare := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", nil))
	for _, name := range []string{"Approved?", "Refund policy", "Approval withdrawn", "Ask each reviewer"} {
		if !refusalMentions(bare, name) {
			t.Errorf("with no value given, nothing refuses for “%s”:%s", name, lines(bare.Refusals))
		}
	}
}

// A field's name is its author's to choose, and a waive sets none longer than
// a plan can list. A request that gives such a name is malformed — told which
// name, in a preview as in an apply — and one of exactly that length is not.
func TestAnOutputNamedLongerThanAPlanListsANameIsRefusedBeforeAnythingIsPlanned(t *testing.T) {
	h := newEngineHarness(t, "Waive Long Output Name Project")
	w := newWaiver(h)
	id := w.start(t, claimWithAGateway(h.projID, "claim-long-output"), nil)
	before := everyRow(t, h)

	tooLong := strings.Repeat("ü", 256)
	want := apierr.Invalidf("“%s…” is too long a name for a waive to set: a field's name is at most 255 characters", strings.Repeat("ü", 64))
	for what, dryRun := range map[string]bool{"a preview": true, "an apply": false} {
		command := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true, tooLong: 1})
		command.DryRun, command.VisitKey = dryRun, "dv1-any-key-at-all-it-is-not-looked-at"
		out, err := w.svc.DeviateInstance(w.ctx, command)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
			t.Errorf("%s giving a name of 256 characters: got\n  %v\nwant exactly\n  %v", what, err, want)
		}
		if out.Applied || len(out.Plan.Refusals) != 0 || out.Plan.VisitKey != "" {
			t.Errorf("%s giving a name of 256 characters was answered with a plan: %+v", what, out)
		}
	}
	// One character shorter is a name like any other: the plan is made, and
	// refuses it for what it is — a field the form does not declare.
	fits := strings.Repeat("ü", 255)
	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true, fits: 1}))
	if plan.Applicable() || !refusalMentions(plan, "form does not declare") {
		t.Errorf("a name of 255 characters the form does not declare: refusals:%s", lines(plan.Refusals))
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("refused requests changed %v", changed)
	}
}
