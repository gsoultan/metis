package bpmn_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// migrateSeconded applies a migration that has to wait for a second
// administrator — it skips a step, takes a control, redirects past one or
// loosens a rule — as an organization with two administrators gets one done:
// asked for by dita and approved by budi. It requires that the plan really
// waited: applied on one call, it fails the test.
//
// It is the test that says the migration has to wait, by calling this, and
// not the plan: a plan that can be applied and does not ask for a second
// administrator fails the test here. Were the plan's word taken for it, a
// planner that stopped asking would send every test that comes through here
// down the one-call path, and each would pass. A migration one administrator
// applies is applied with MigrateInstances, by the test itself.
//
// It is for a test that needs a migration to have happened and is about what
// follows. That a skip waits for a second administrator, and what an approved
// run records, is tests/instancemigration's to say.
func migrateSeconded(t *testing.T, h engineHarness, v1, v2 uuid.UUID, mapping map[string]string, opts ...servicecontracts.MigrationOption) error {
	t.Helper()
	ctx := h.Ctx()
	plan, err := h.svc.PlanInstanceMigration(ctx, v1, v2, mapping, opts...)
	if err != nil || !plan.Applicable() {
		// A plan that cannot be made, or that refuses, asks nobody: the
		// apply's own refusal is what the test reads.
		return h.svc.MigrateInstances(ctx, v1, v2, mapping, opts...)
	}
	if !plan.RequiresSecondApprover {
		t.Fatalf("this test applies its migration as one a second administrator has to approve, and the plan does not ask for one: "+
			"the planner no longer asks, or the test should apply it on one call (%+v)", plan)
	}
	// What needs a second administrator must not go through on one call: the
	// apply itself has to refuse it, having moved nothing, or every test that
	// comes through here would pass over a gate that was not there.
	if err := h.svc.MigrateInstances(ctx, v1, v2, mapping, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a migration that needs a second administrator was answered %v on one administrator's call, want it forbidden", err)
	}
	w := newWaiver(h)
	pending, err := h.svc.RequestMigrationApproval(w.as("dita"), v1, v2, mapping, opts...)
	if err != nil {
		return err
	}
	if pending.Status != entities.DeviationRequestPending || pending.RequestID == uuid.Nil {
		t.Fatalf("asking for the migration answered %+v, want a request waiting for approval", pending)
	}
	// The harness's facade is built without the service of requests; the
	// waiver's own is the one its approvals go through.
	out, err := w.approvals.ApproveDeviationRequest(w.as("budi"), pending.RequestID, "")
	if err == nil && !out.Applied {
		t.Fatalf("the approved migration was not applied: %+v", out)
	}
	return err
}
