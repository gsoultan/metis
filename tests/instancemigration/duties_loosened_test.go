package instancemigration

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A version that takes away a separation-of-duties rule loosens an approval
// rule for every instance that has still to pass the step: whoever did the
// one half may then do the other. That is not one administrator's call.

// fourEyesRuled is fourEyes with the approval's rule as given: "" for none.
func fourEyesRuled(f *fixture, rule string) *entities.ProcessDefinition {
	def := fourEyes(f.project, "submit", "approve")
	for _, node := range def.Nodes {
		if node.ID != "approve" {
			continue
		}
		node.Properties = map[string]any{}
		if rule != "" {
			node.Properties["separation_of_duties"] = rule
		}
	}
	return def
}

// TestAVersionThatDropsASeparationOfDutiesRuleIsNotOneAdministratorsCall.
//
// Root cause: a migration asked for a second administrator for a skip and for
// a control not carried across, and looked at nothing else of what the new
// version loosens. A rule that the person who submitted a request may not
// approve it was simply gone after the migration, with a warning at most —
// and ada, who had submitted, approved her own request.
func TestAVersionThatDropsASeparationOfDutiesRuleIsNotOneAdministratorsCall(t *testing.T) {
	for name, rule := range map[string]string{
		// The rule is gone from the step.
		"the rule removed": "",
		// The rule is there and names only a step the new version does not
		// have: it can no longer refuse anybody.
		"the rule left naming a step that is gone": "review",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyesRuled(f, rule))
			f.completeTaskOn(t, "submit", "ada")
			instance := f.assertWaitingAt(t, v1, "approve")
			actor := servicecontracts.WithActor("dita")

			plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor)
			if err != nil || !plan.Applicable() {
				t.Fatalf("the plan: %+v %v", plan, err)
			}
			if !plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 1 {
				t.Fatalf("a version that drops the rule needs nobody else: requires=%v reasons=%v", plan.RequiresSecondApprover, plan.SecondApproverReasons)
			}
			want := "“Approve the request” would no longer be refused to whoever performed “Submit the request”: " +
				"the new version does not keep that separation of duties, and 1 instance(s) have not passed “Approve the request”"
			if plan.SecondApproverReasons[0] != want {
				t.Fatalf("the reason reads\n  %s\nwant\n  %s", plan.SecondApproverReasons[0], want)
			}
			// Not a control dropped: nothing is held and nothing acknowledged.
			if len(plan.ComplianceHolds) != 0 {
				t.Fatalf("the plan holds %+v for a rule that is not a control step", plan.ComplianceHolds)
			}
			if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, actor); !errors.Is(err, apierr.ErrForbidden) {
				t.Fatalf("the migration on one administrator's call: %v, want it forbidden", err)
			}
			f.assertWaitingAt(t, v1, "approve")
			f.assertNoMigrationEntries(t, instance.ID)

			result, err := f.applyWithApproval(t, v1, v2, nil, actor)
			if err != nil || result.Changed != 1 {
				t.Fatalf("the approved migration: %+v %v", result, err)
			}
			f.assertWaitingAt(t, v2, "approve")
		})
	}
}

// What is not a loosening asks nobody: the rule kept, the rule kept under the
// steps' new names, a rule that gains a step, and a rule dropped for a step
// every instance has already passed.
func TestAVersionThatKeepsItsSeparationOfDutiesAsksNobody(t *testing.T) {
	actor := servicecontracts.WithActor("dita")
	t.Run("the rule kept", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyes(f.project, "submit", "approve"))
		f.completeTaskOn(t, "submit", "ada")
		if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor); err != nil || plan.RequiresSecondApprover {
			t.Fatalf("a version that keeps the rule: requires=%v reasons=%v (err %v)", plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
		}
		if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, actor); err != nil {
			t.Fatalf("on one call: %v", err)
		}
		f.assertWaitingAt(t, v2, "approve")
	})
	t.Run("the rule kept under the steps' new names", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyes(f.project, "request", "signOff"))
		f.completeTaskOn(t, "submit", "ada")
		rename := map[string]string{"submit": "request", "approve": "signOff"}
		if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, rename, actor); err != nil || plan.RequiresSecondApprover {
			t.Fatalf("a version that renames the steps and the rule with them: requires=%v reasons=%v (err %v)", plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
		}
	})
	t.Run("a rule that names one step more", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyesRuled(f, "submit, start"))
		f.completeTaskOn(t, "submit", "ada")
		if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor); err != nil || plan.RequiresSecondApprover {
			t.Fatalf("a version that tightens the rule: requires=%v reasons=%v (err %v)", plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
		}
	})
	t.Run("the rule dropped once everybody has passed the step", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyesRuled(f, ""))
		f.completeTaskOn(t, "submit", "ada")
		f.completeTaskOn(t, "approve", "bo")
		plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, actor)
		if err != nil || plan.RequiresSecondApprover {
			t.Fatalf("a rule dropped for a step nobody has still to pass: requires=%v reasons=%v (err %v)", plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
		}
	})
}
