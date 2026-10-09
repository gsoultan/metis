package instancemigration

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The gate is in the service: no skip, and no migration that drops a control,
// is applied on one administrator's call — whichever way the apply is
// reached and whatever it carries (design §9.6, rulings addendum §12).
//
// Every case below ends the same way: forbidden, the instance still running
// the version it was started on with its step open, and nothing in its
// ledger or on its trail.
func TestAnApplyRefusesASkipWithoutAnApprovedRequest(t *testing.T) {
	forged := func(approval servicecontracts.MigrationApproval) servicecontracts.MigrationOption {
		return func(o *servicecontracts.MigrationOptions) { o.Approval = approval }
	}
	omar := accountOf("omar")
	type attempt struct {
		name string
		// with answers the options the apply is made with, beside the skip.
		with func(t *testing.T, f *fixture, v1, v2 uuid.UUID) []servicecontracts.MigrationOption
		says string
	}
	attempts := []attempt{
		{"nothing offered", func(*testing.T, *fixture, uuid.UUID, uuid.UUID) []servicecontracts.MigrationOption { return nil },
			"a second administrator has to approve it first; nothing was moved"},
		{"a request that does not exist", func(*testing.T, *fixture, uuid.UUID, uuid.UUID) []servicecontracts.MigrationOption {
			return []servicecontracts.MigrationOption{servicecontracts.WithApprovedRequest(uuid.Must(uuid.NewV7()))}
		}, "does not exist here; nothing was moved"},
		{"a request that still waits", func(t *testing.T, f *fixture, v1, v2 uuid.UUID) []servicecontracts.MigrationOption {
			pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("a")...)
			if err != nil {
				t.Fatalf("ask: %v", err)
			}
			return []servicecontracts.MigrationOption{servicecontracts.WithApprovedRequest(pending.RequestID)}
		}, "is pending_approval, not an approved migration; nothing was moved"},
		{"an approval nobody stored", func(*testing.T, *fixture, uuid.UUID, uuid.UUID) []servicecontracts.MigrationOption {
			return []servicecontracts.MigrationOption{forged(servicecontracts.MigrationApproval{
				RequestedBy: "dita", RequestedByID: accountOf("dita"), ApprovedBy: "omar", ApprovedByID: omar, DecidedAt: time.Now()})}
		}, "a second administrator has to approve it first; nothing was moved"},
		{"an approval nobody stored, naming a request", func(*testing.T, *fixture, uuid.UUID, uuid.UUID) []servicecontracts.MigrationOption {
			return []servicecontracts.MigrationOption{forged(servicecontracts.MigrationApproval{
				RequestID: uuid.Must(uuid.NewV7()), RequestedBy: "dita", RequestedByID: accountOf("dita"),
				ApprovedBy: "omar", ApprovedByID: omar, DecidedAt: time.Now()})}
		}, "does not exist here; nothing was moved"},
		{"an approval nobody stored, naming a request that still waits", func(t *testing.T, f *fixture, v1, v2 uuid.UUID) []servicecontracts.MigrationOption {
			pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("a")...)
			if err != nil {
				t.Fatalf("ask: %v", err)
			}
			return []servicecontracts.MigrationOption{forged(servicecontracts.MigrationApproval{
				RequestID: pending.RequestID, RequestedBy: "dita", RequestedByID: accountOf("dita"),
				ApprovedBy: "omar", ApprovedByID: omar, DecidedAt: time.Now()})}
		}, "is pending_approval, not an approved migration; nothing was moved"},
	}
	for _, attempt := range attempts {
		t.Run(attempt.name, func(t *testing.T) {
			f := newFixture(t)
			first, second := f.parkedOnOpsApprove(t)
			v1, v2 := uuidOf(t, first), uuidOf(t, second)
			opts := append(slices.Clip(skipOps("a")), attempt.with(t, f, v1, v2)...)
			calls := map[string]func() error{
				"ApplyInstanceMigration": func() error { _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...); return err },
				"MigrateInstances":       func() error { return f.svc.MigrateInstances(f.ctx, v1, v2, nil, opts...) },
				// Signed in as an administrator, and as the approver named, it is
				// refused all the same: nothing about the caller is an approval.
				"ApplyInstanceMigration as omar": func() error {
					_, err := f.svc.ApplyInstanceMigration(adminAs(f.ctx, "omar"), v1, v2, nil, opts...)
					return err
				},
			}
			for name, call := range calls {
				// Sent twice: a retry is refused as the first was.
				for range 2 {
					err := call()
					if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), attempt.says) {
						t.Fatalf("%s: %v, want it forbidden: %q", name, err, attempt.says)
					}
					f.assertNothingMoved(t, v1)
				}
			}
		})
	}

	// A request that was approved and has been carried out is spent: it is no
	// approval of the same migration made again, for the same reason or another.
	t.Run("a request already applied", func(t *testing.T) {
		f := newFixture(t)
		v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		park := func() {
			t.Helper()
			if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
				t.Fatalf("start a quotation: %v", err)
			}
			f.completeTaskOn(t, "supervisorReview", "sam")
		}
		park()
		// Staged: version 1 stays the one a new quotation starts on.
		v2, err := f.svc.DeployDefinition(f.ctx, quotationV2(f), false)
		if err != nil {
			t.Fatalf("stage v2: %v", err)
		}
		pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("a")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		if out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, ""); err != nil || !out.Applied {
			t.Fatalf("approve: %+v %v", out, err)
		}
		park()
		waiting := f.stillOn(t, v1.String())
		if len(waiting) != 1 {
			t.Fatalf("%d instance(s) are on v1, want the one parked after the run", len(waiting))
		}
		for _, reason := range []string{"a", "b"} {
			under := append(slices.Clip(skipOps(reason)), servicecontracts.WithApprovedRequest(pending.RequestID))
			_, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
			if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "is applied, not an approved migration; nothing was moved") {
				t.Fatalf("an apply for reason %q under a request already applied: %v, want it forbidden", reason, err)
			}
			if on := f.stillOn(t, v1.String()); len(on) != 1 || on[0].ID != waiting[0].ID || len(f.ledger(t, on[0].ID)) != 0 {
				t.Fatalf("the refused apply moved or decided the instance parked after the run")
			}
			f.assertNoMigrationEntries(t, waiting[0].ID)
		}
	})
}

// What an approval covers is the migration that was asked for, by whoever
// asked. While a request is approved and its run is going, an apply that
// carries its id for another migration — the same step skipped for another
// reason — or that names somebody else as having authorised it, is refused,
// and the approved run goes on and records what was approved.
func TestAnApprovedRequestIsNoApprovalOfAnotherMigration(t *testing.T) {
	f, listing := newRacedFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("a")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	under := servicecontracts.WithApprovedRequest(pending.RequestID)
	var anotherReason, anotherActor, anotherMapping error
	listing.atTheApprovedApplysListing(func() {
		_, anotherReason = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, append(slices.Clip(skipOps("b")), under)...)
		_, anotherActor = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, append(slices.Clip(skipOps("a")), servicecontracts.WithActor("mallory"), under)...)
		_, anotherMapping = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, map[string]string{"salesApprove": "salesApprove"}, append(slices.Clip(skipOps("a")), under)...)
	})
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	reached(t, listing)
	for name, refused := range map[string]error{"another reason": anotherReason, "another mapping": anotherMapping} {
		if !errors.Is(refused, apierr.ErrForbidden) || !strings.Contains(refused.Error(), "does not cover this migration") ||
			!strings.Contains(refused.Error(), "no longer the one that was asked for") {
			t.Fatalf("%s under the approved request: %v, want it forbidden as another migration", name, refused)
		}
	}
	if !errors.Is(anotherActor, apierr.ErrForbidden) || !strings.Contains(anotherActor.Error(), "was asked for by dita") {
		t.Fatalf("the same migration naming another authoriser: %v, want it forbidden", anotherActor)
	}
	if err != nil || !out.Applied || out.MigrationResult.Changed != 1 {
		t.Fatalf("the approved run: %+v %v, want it to go on and move the instance", out, err)
	}
	instance := f.onlyInstance(t)
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Reason != "a" || rows[0].Actor != "dita" || rows[0].ApprovedBy != "omar" {
		t.Fatalf("the ledger holds %+v, want the one skip that was approved", rows)
	}
}
