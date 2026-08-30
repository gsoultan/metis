package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A process consulting a decision table, and routing on the answer.
//
// This is the join between the two halves of the product, and it appeared in
// the tests exactly once — as a node type in a round-trip check. The evaluator
// has been verified on its own, and the designer could not save a decision key
// at all until recently, so nothing had ever run a process that asks a decision
// something and then acts on what it says.
//
// The table is the one from docs/data-flow.md: under 100 needs nobody, under
// 1000 a manager, anything more a director.

func createExpenseDecision(t *testing.T, h *serviceTaskHarness) {
	t.Helper()
	if _, err := h.decisionSvc.CreateDecision(t.Context(), entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "expense-approval-level",
		Name:      "Expense approval level",
		HitPolicy: entities.HitPolicyFirst,
		Inputs: []entities.DecisionInput{
			{ID: "in_amount", Label: "Amount", Expression: "amount", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{
			{ID: "out_level", Label: "Approval level", Name: "approvalLevel", Type: "string"},
			{ID: "out_approver", Label: "Approver", Name: "approver", Type: "string"},
		},
		Rules: []entities.DecisionRule{
			{ID: "r1", Inputs: []string{"< 100"}, Outputs: []any{"auto", "system"}},
			{ID: "r2", Inputs: []string{"< 1000"}, Outputs: []any{"manager", "line-manager"}},
			{ID: "r3", Inputs: []string{"-"}, Outputs: []any{"director", "finance-director"}},
		},
	}); err != nil {
		t.Fatalf("create the decision: %v", err)
	}
}

// start → decide → route → (manager | director | straight to the end)
func expenseProcess(key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Key:  key,
		Name: "Expense approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Expense submitted"},
			{ID: "decide", Type: entities.BusinessRuleTask, Name: "Decide approval level",
				Properties: map[string]any{"decision_key": "expense-approval-level"}},
			{ID: "route", Type: entities.ExclusiveGateway, Name: "Who approves?"},
			{ID: "manager", Type: entities.UserTask, Name: "Manager approves", Assignee: "carol"},
			{ID: "director", Type: entities.UserTask, Name: "Director approves", Assignee: "dave"},
			{ID: "end", Type: entities.EndEvent, Name: "Decision recorded"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "route"},
			{ID: "f3", SourceRef: "route", TargetRef: "manager", Condition: "approvalLevel = manager"},
			{ID: "f4", SourceRef: "route", TargetRef: "director", Condition: "approvalLevel = director"},
			{ID: "f5", SourceRef: "route", TargetRef: "end", Condition: "approvalLevel = auto"},
			{ID: "f6", SourceRef: "manager", TargetRef: "end"},
			{ID: "f7", SourceRef: "director", TargetRef: "end"},
		},
	}
}

// The results have to reach the process, or the gateway after it has nothing to
// read and the whole arrangement is decoration.
func TestBusinessRuleTask_PutsTheDecisionsResultsIntoTheProcess(t *testing.T) {
	h := newServiceTaskHarness(t)
	createExpenseDecision(t, h)

	instance := h.runDefinition(t, expenseProcess("expense-results"), map[string]any{"amount": float64(2400)})

	if got := instance.Variables["approvalLevel"]; got != "director" {
		t.Errorf("approvalLevel = %#v, want \"director\" — the decision's answer did not reach the process", got)
	}
	if got := instance.Variables["approver"]; got != "finance-director" {
		t.Errorf("approver = %#v, want \"finance-director\"", got)
	}
	// What it started with is still there: a decision adds, it does not replace.
	if got := instance.Variables["amount"]; got != float64(2400) {
		t.Errorf("amount = %#v after the decision ran", got)
	}
}

// The point of the join: what the table says decides where the process goes.
func TestBusinessRuleTask_TheAnswerDecidesWhichPathIsTaken(t *testing.T) {
	cases := []struct {
		amount float64
		want   string // the task that should be waiting, or "" for none
	}{
		{2400, "Director approves"},
		{500, "Manager approves"},
		{40, ""}, // approved without anyone being asked
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			h := newServiceTaskHarness(t)
			createExpenseDecision(t, h)

			instance := h.runDefinition(t, expenseProcess("expense-route"), map[string]any{"amount": tc.amount})
			tasks := h.tasksFor(t, instance.ID)

			if tc.want == "" {
				if len(tasks) != 0 {
					t.Fatalf("an expense of %v asked %v; it should need nobody", tc.amount, taskNames(tasks))
				}
				if instance.Status != entities.ProcessCompleted {
					t.Errorf("an expense of %v left the process %q", tc.amount, instance.Status)
				}
				return
			}

			if len(tasks) != 1 || tasks[0].Name != tc.want {
				t.Fatalf("an expense of %v produced %v, want [%s]", tc.amount, taskNames(tasks), tc.want)
			}
		})
	}
}

// The names on either side need not match, and when they do not the mapping is
// the only thing that connects them.
func TestBusinessRuleTask_TranslatesNamesInBothDirections(t *testing.T) {
	h := newServiceTaskHarness(t)
	createExpenseDecision(t, h)

	def := expenseProcess("expense-mapped")
	for _, node := range def.Nodes {
		if node.ID == "decide" {
			node.Properties = map[string]any{
				"decision_key": "expense-approval-level",
				// The process calls it claimTotal; the table looks up amount.
				"input_mapping": map[string]any{"amount": "claimTotal"},
				// The table answers approvalLevel; the process wants whoSignsOff.
				"output_mapping": map[string]any{"whoSignsOff": "approvalLevel"},
			}
		}
	}
	// Route on the process's own name for it.
	for _, flow := range def.Flows {
		switch flow.ID {
		case "f3":
			flow.Condition = "whoSignsOff = manager"
		case "f4":
			flow.Condition = "whoSignsOff = director"
		case "f5":
			flow.Condition = "whoSignsOff = auto"
		}
	}

	// 500 is chosen because it distinguishes: it reaches the manager row only if
	// claimTotal actually arrives as amount. With the input mapping broken the
	// table sees no amount at all, falls through to the catch-all row, and
	// answers director — which an amount of 2400 would have answered anyway.
	instance := h.runDefinition(t, def, map[string]any{"claimTotal": float64(500)})

	if got := instance.Variables["whoSignsOff"]; got != "manager" {
		t.Errorf("whoSignsOff = %#v, want \"manager\" — the answer did not come back under the name the process uses", got)
	}
	if tasks := h.tasksFor(t, instance.ID); len(tasks) != 1 || tasks[0].Name != "Manager approves" {
		t.Errorf("the process took %v, want [Manager approves]", taskNames(tasks))
	}
}

// A decision that does not exist is a modelling mistake, and stopping is the
// right answer: carrying on would route on a value that was never decided.
func TestBusinessRuleTask_ReportsADecisionThatDoesNotExist(t *testing.T) {
	h := newServiceTaskHarness(t)
	ctx := t.Context()

	def := expenseProcess("expense-missing-decision")
	def.Project = &entities.Project{ID: h.projectID}
	for _, node := range def.Nodes {
		if node.ID == "decide" {
			node.Properties = map[string]any{"decision_key": "no-such-decision"}
		}
	}
	if _, err := h.defSvc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.engine.StartProcess(ctx, h.projectID, def.Key, map[string]any{"amount": float64(100)})
	if err == nil {
		t.Fatal("a process consulting a decision that does not exist ran anyway")
	}
}

// A step told to apply a decision, without saying which, is not the same as a
// step that does nothing — it is unfinished, and should say so.
func TestBusinessRuleTask_ReportsWhenNoDecisionIsNamed(t *testing.T) {
	h := newServiceTaskHarness(t)
	ctx := t.Context()

	def := expenseProcess("expense-no-decision")
	def.Project = &entities.Project{ID: h.projectID}
	for _, node := range def.Nodes {
		if node.ID == "decide" {
			node.Properties = nil
		}
	}
	if _, err := h.defSvc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.engine.StartProcess(ctx, h.projectID, def.Key, map[string]any{"amount": float64(100)})
	if err == nil {
		t.Fatal("a business rule task with no decision named ran as though it had one")
	}
}

// Two decisions can share a key across versions; the current one is the one to
// apply unless the step asks for another.
func TestBusinessRuleTask_AppliesTheCurrentVersionOfTheTable(t *testing.T) {
	h := newServiceTaskHarness(t)
	createExpenseDecision(t, h)

	// A second version, where everything needs a director.
	if _, err := h.decisionSvc.CreateDecision(t.Context(), entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "expense-approval-level",
		Name:      "Expense approval level",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "in_amount", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{
			{ID: "out_level", Name: "approvalLevel", Type: "string"},
			{ID: "out_approver", Name: "approver", Type: "string"},
		},
		Rules: []entities.DecisionRule{
			{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"director", "finance-director"}},
		},
	}); err != nil {
		t.Fatalf("create the second version: %v", err)
	}

	// An amount that the first version sent to a manager.
	instance := h.runDefinition(t, expenseProcess("expense-versioned"), map[string]any{"amount": float64(500)})

	if got := instance.Variables["approvalLevel"]; got != "director" {
		t.Errorf("approvalLevel = %#v; the process applied an older version of the table", got)
	}
}
