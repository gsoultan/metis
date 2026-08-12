package decision_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// A decision with no key must be rejected rather than stored.
//
// CreateDecisionRequest carries `Decision entities.DecisionDefinition` by
// value, so a request body that omits the "decision" envelope decodes to the
// zero value instead of failing. CreateDecision then stored it, producing a row
// with an empty key that no process can ever reference.
//
// It compounds: the version bump looks the key up first, so the second keyless
// request finds the first one and stores version 2. A client sending the wrong
// shape gets 200 and a fresh id every time while silently filling the table.
//
// Found while importing docs/examples/ with the wrong envelope — four such rows
// accumulated before anything looked wrong, and the only symptom was a process
// failing later with "could not get decision by key: record not found".
func TestCreateDecision_RejectsADecisionWithNoKey(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	svc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	ctx := t.Context()

	_, err := svc.CreateDecision(ctx, entities.DecisionDefinition{})
	if err == nil {
		t.Fatal("a decision with no key was accepted")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Fatalf("error does not name the missing field: %v", err)
	}

	// The point of rejecting is that nothing is written.
	all, err := repo.Decision().List(ctx)
	if err != nil {
		t.Fatalf("list decisions: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("%d rows were stored despite the request being rejected", len(all))
	}
}

func TestCreateDecision_AcceptsAKeyedDecision(t *testing.T) {
	db := testutils.SetupTestDB(t)
	svc := serviceimpl.NewDecisionService(repositories.NewRepository(db),
		serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))

	id, err := svc.CreateDecision(t.Context(), entities.DecisionDefinition{
		Key:       "expense-approval-level",
		Name:      "Expense approval level",
		HitPolicy: "FIRST",
		Outputs:   []entities.DecisionOutput{{ID: "out", Name: "approvalLevel", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"< 100"}, Outputs: []any{"auto"}}},
	})
	if err != nil {
		t.Fatalf("a valid decision was rejected: %v", err)
	}
	if id.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("no id was assigned")
	}
}
