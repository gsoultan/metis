package instancemigration

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// TestAnApprovedRunItsPlanRefusesIsARefusalAndNotAServerFailure.
//
// Root cause: what an approver is told when the approved run does not finish
// was built as a new error with no cause under it. A run the plan refuses —
// the instance moved on, between the approval and the run, to a step the new
// version does not have — is a refusal of what was asked, a 400 on any other
// path; told that way it had no class left and was answered as the server's
// own failure, a 500 that counts against the error budget.
func TestAnApprovedRunItsPlanRefusesIsARefusalAndNotAServerFailure(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, prepareStep, signStep))
	f.assertWaitingAt(t, v1, "prepare")
	dita := adminAs(f.ctx, "dita")
	opts := []servicecontracts.MigrationOption{servicecontracts.WithAcknowledgedHolds("control"), servicecontracts.WithActor("dita")}
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil || pending.Status != entities.DeviationRequestPending {
		t.Fatalf("ask: %+v %v", pending, err)
	}
	// The approval plans the migration again, with the instance waiting at
	// the preparation, and approves. The run then plans as any apply does:
	// it has listed the instance — the second listing — when the
	// preparation's holder finishes it, and reads the instance's work after
	// that. The work now waits at the control, which the new version does not
	// have and the migration says nothing about.
	listing.on, listing.fire = listing.calls+2, func() { f.completeTaskOn(t, "prepare", "ada") }
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	reached(t, listing)

	if !errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrForbidden) || out.Applied {
		t.Fatalf("the run its plan refused: applied=%v err=%v, want it answered as a refusal of what was asked", out.Applied, err)
	}
	said := err.Error()
	if !strings.HasPrefix(said, "invalid argument: the approved migration did not finish: ") || !strings.Contains(said, "has nowhere to put the work parked on control") ||
		!strings.HasSuffix(said, "Request "+pending.RequestID.String()+" now reads interrupted; what its run had done stands, and what remains has to be asked for again") {
		t.Fatalf("the refusal reads: %s", said)
	}
	// One class, said once, and one thing to do.
	if strings.Count(said, "invalid argument") != 1 || strings.Contains(said, "run the same migration again") {
		t.Fatalf("the refusal says its class twice, or to run it again: %s", said)
	}
	read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
	if err != nil || read.Status != entities.DeviationRequestInterrupted || read.DecidedBy != "omar" || read.Outcome["changed"] != float64(0) {
		t.Fatalf("the request reads %q with %v (err %v), want interrupted with nothing changed", read.Status, read.Outcome, err)
	}
	// The request says the run was refused before it moved anything — not
	// that it stopped on a failure after acting on no instance, which is what
	// is said of a run that began and broke.
	if said, _ := read.Outcome["error"].(string); said != "the run was refused before it moved anything: the plan made when it came to start refused the migration" {
		t.Fatalf("the request's outcome says %q; want it to say the run was refused before it moved anything, by its plan", said)
	}
	instance := f.assertWaitingAt(t, v1, "control")
	f.assertNoMigrationEntries(t, instance.ID)
}
