package instancemigration

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// A separation-of-duties rule is loosened by a migration when the migration
// changes whom it refuses. A rule that reads the same before and after, over
// steps that are the same before and after, is not — whatever it names.

// TestAnUnchangedRuleNamingAStepNeitherVersionHasAsksNobody.
//
// Root cause: a step a rule names counted as lost whenever the new version
// did not have it, without asking whether the old one did. A rule left naming
// a step some earlier version removed made every later migration wait for a
// second administrator, with a sentence that was not true: nothing changes,
// the rule refused nobody on that name before and refuses nobody on it after.
func TestAnUnchangedRuleNamingAStepNeitherVersionHasAsksNobody(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, fourEyesRuled(f, "submit, opsApprove"), fourEyesRuled(f, "submit, opsApprove"))
	f.completeTaskOn(t, "submit", "ada")
	f.assertWaitingAt(t, v1, "approve")
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: applicable=%v refusals=%v (err %v)", plan.Applicable(), plan.Refusals, err)
	}
	if plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 0 {
		t.Fatalf("a rule the migration does not change waits for a second administrator: %v", plan.SecondApproverReasons)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, actor); err != nil {
		t.Fatalf("the migration on one administrator's call: %v", err)
	}
	f.assertWaitingAt(t, v2, "approve")
	if n := f.requestCount(t); n != 0 {
		t.Fatalf("it left %d request(s)", n)
	}
	// And the rule refuses whom it refused.
	approval := f.openTasks(t)[0]
	if err := f.svc.CompleteTask(f.ctx, approval.ID, "ada", nil); !errors.Is(err, impl.ErrTaskForbidden) {
		t.Errorf("ada, who did the submit, completes the approval: err=%v, want it refused", err)
	}
}

// approvalAlone is the four-eyes process without its first half: the approval
// only, its rule still naming the submit the version no longer has.
func approvalAlone(f *fixture) *entities.ProcessDefinition {
	def := fourEyes(f.project, "submit", "approve")
	start, approve, end := def.Nodes[0], def.Nodes[2], def.Nodes[3]
	approve.Incoming = []string{"e1"}
	def.Nodes = []*entities.Node{start, approve, end}
	def.Flows = []*entities.SequenceFlow{
		{ID: "e1", SourceRef: "start", TargetRef: "approve"},
		{ID: "e3", SourceRef: "approve", TargetRef: "end"},
	}
	return def
}

// A rule that still names a step the new version drops is loosened, and the
// plan says how: whoever performed the step before the migration is still
// refused; nobody can perform it afterwards. The sentence said the opposite of
// the first half — that whoever performed it would no longer be refused.
func TestARuleLeftNamingAStepTheNewVersionDropsSaysWhatIsLost(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), approvalAlone(f))
	f.completeTaskOn(t, "submit", "ada")
	f.assertWaitingAt(t, v1, "approve")
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: applicable=%v refusals=%v (err %v)", plan.Applicable(), plan.Refusals, err)
	}
	want := "“Approve the request” may not be done by whoever performed “Submit the request”, which the new version no longer has: " +
		"whoever performed it before the migration is still refused, but nobody can perform it afterwards, so the rule refuses nobody new — " +
		"and 1 instance(s) have not passed “Approve the request”"
	if !plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 1 || plan.SecondApproverReasons[0] != want {
		t.Fatalf("the reasons read\n  %v\nwant\n  %s", plan.SecondApproverReasons, want)
	}
	if result, err := f.applyWithApproval(t, v1, v2, nil, actor); err != nil || result.Changed != 1 {
		t.Fatalf("the approved migration: %+v %v", result, err)
	}
	f.assertWaitingAt(t, v2, "approve")
	// What the sentence says is so: ada, who submitted before the migration,
	// is still refused the approval; bo is not.
	approval := f.openTasks(t)[0]
	if err := f.svc.ClaimTask(f.ctx, approval.ID, "ada"); !errors.Is(err, impl.ErrTaskForbidden) {
		t.Errorf("ada, who did the submit, claims the approval: err=%v, want it refused", err)
	}
	if err := f.svc.CompleteTask(f.ctx, approval.ID, "bo", nil); err != nil {
		t.Fatalf("bo, who did not, completes the approval: %v", err)
	}
}
