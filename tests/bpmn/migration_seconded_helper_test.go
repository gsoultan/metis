package bpmn_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// migrateSeconded applies a migration as an organization with two
// administrators gets one done: a plan that needs a second administrator — it
// skips a step, or drops a control — is asked for by dita and approved by
// budi, and any other plan is applied on the one call, as it always was. It
// requires that such a plan really waited: applied on one call, it fails the
// test.
//
// It is for a test that needs a migration to have happened and is about what
// follows. That a skip waits for a second administrator, and what an approved
// run records, is tests/instancemigration's to say.
func migrateSeconded(t *testing.T, h engineHarness, v1, v2 uuid.UUID, mapping map[string]string, opts ...servicecontracts.MigrationOption) error {
	t.Helper()
	ctx := h.Ctx()
	plan, err := h.svc.PlanInstanceMigration(ctx, v1, v2, mapping, opts...)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover {
		return h.svc.MigrateInstances(ctx, v1, v2, mapping, opts...)
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
