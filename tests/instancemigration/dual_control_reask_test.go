package instancemigration

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A request the clock has closed does not hold its migration until the sweep
// comes round. Asking again closes it — as expired, by the function the sweep
// closes it with — and makes a fresh request in the same call. No sweep runs
// anywhere in this test.
func TestAMigrationCanBeAskedAgainOnceItsRequestHasExpired(t *testing.T) {
	for _, who := range []string{"dita", "omar"} {
		t.Run("asked again by "+who, func(t *testing.T) {
			f := newFixture(t)
			_, v1, v2, overdue := f.askToSkipOps(t)
			f.letTimePass(t, overdue, 1)

			fresh, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, who), v1, v2, nil, skipOps("the role was eliminated")...)
			if err != nil || fresh.RequestID == overdue || fresh.Status != entities.DeviationRequestPending || fresh.RequestedBy != who {
				t.Fatalf("asking again past the deadline: %+v %v, want a fresh request by %s", fresh, err, who)
			}
			if !fresh.ExpiresAt.After(time.Now()) {
				t.Fatalf("the fresh request's deadline %s is not ahead", fresh.ExpiresAt)
			}
			if got := f.storedStatus(t, overdue); got != "expired" {
				t.Fatalf("the overdue request is %s, want it closed as expired", got)
			}
			if n := f.requestCount(t); n != 2 {
				t.Fatalf("there are %d request(s), want the expired one and the fresh one", n)
			}
			f.assertNothingMoved(t, v1)
			// The fresh one is an ordinary request: asked again it is itself.
			again, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, who), v1, v2, nil, skipOps("the role was eliminated")...)
			if err != nil || again.RequestID != fresh.RequestID {
				t.Fatalf("the fresh request asked again: %+v %v", again, err)
			}
		})
	}
}

// A request left approved by a process that died — approved, its run never
// reported, its window closed — does not keep its migration from being asked
// for again. Asking again records that the run never reported, and makes a
// fresh request, which is approved and run as any is. No sweep runs anywhere
// in this test.
func TestAMigrationLeftApprovedByADeadProcessCanBeAskedAgain(t *testing.T) {
	f := newFixture(t)
	dita, v1, v2, dead := f.askToSkipOps(t)
	f.leftApproved(t, dead, "2 hours")

	fresh, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil || fresh.RequestID == dead || fresh.Status != entities.DeviationRequestPending {
		t.Fatalf("asking again for a migration a dead process left approved: %+v %v, want a fresh request", fresh, err)
	}
	read, err := f.svc.GetDeviationRequest(dita, dead)
	if err != nil || f.storedStatus(t, dead) != "interrupted" || read.Outcome["error"] != "the run did not report back" || read.DecidedBy != "omar" {
		t.Fatalf("the dead request is stored %s with %v (err %v), want it interrupted, its run said never to have reported, still approved by omar",
			f.storedStatus(t, dead), read.Outcome, err)
	}
	f.assertNothingMoved(t, v1)

	// The dead request is spent: no apply runs under it, before or after.
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), fresh.RequestID, "")
	if err != nil || !out.Applied || out.MigrationResult.Changed != 1 {
		t.Fatalf("approving the fresh request: %+v %v", out, err)
	}
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v2 {
		t.Fatal("the fresh request's run did not move the instance")
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].RequestID != fresh.RequestID {
		t.Fatalf("the ledger names %+v, want the one skip under the fresh request", rows)
	}
}

// An approved request whose run may still be going is not closed by asking
// again: the ask is refused, and told that the migration is being applied.
func TestAMigrationBeingAppliedIsNotAskedForAgain(t *testing.T) {
	f := newFixture(t)
	_, v1, v2, running := f.askToSkipOps(t)
	f.leftApproved(t, running, "10 minutes")
	for _, who := range []string{"dita", "pia"} {
		_, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, who), v1, v2, nil, skipOps("the role was eliminated")...)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "approved by omar and is being applied now") ||
			!strings.Contains(err.Error(), running.String()) {
			t.Fatalf("%s asking for a migration that is being applied: %v", who, err)
		}
	}
	if got := f.storedStatus(t, running); got != "approved" || f.requestCount(t) != 1 {
		t.Fatalf("the refused asks left the request %s among %d, want it approved and alone", got, f.requestCount(t))
	}
}
