package instancemigration

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
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

// A dry run asks nobody: neither the planner nor the route's preview leaves a
// request behind, however often it is run. Only an apply does, once — and the
// route's apply of a skip moves nothing: it asks.
func TestADryRunNeverAsksForApproval(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	dita := adminAs(f.ctx, "dita")
	for range 2 {
		plan, err := f.svc.PlanInstanceMigration(dita, uuidOf(t, v1), uuidOf(t, v2), nil, skipOps("the role was eliminated")...)
		if err != nil || !plan.RequiresSecondApprover {
			t.Fatalf("the plan: %+v %v", plan, err)
		}
		preview := f.asked(t, dita, skipOpsRequest(v1, v2, true))
		if preview.Err != nil || preview.PendingApproval != nil || preview.Applied || !preview.Plan.RequiresSecondApprover {
			t.Fatalf("the route's dry run: %+v", preview)
		}
	}
	if n := f.requestCount(t); n != 0 {
		t.Fatalf("dry runs left %d request(s)", n)
	}
	var first uuid.UUID
	for range 2 {
		sent := f.asked(t, dita, skipOpsRequest(v1, v2, false))
		if sent.Err != nil || sent.PendingApproval == nil || sent.Applied || sent.PassedOver == nil || len(sent.PassedOver) != 0 {
			t.Fatalf("the route's apply: %+v, want it sent for approval with nothing applied and nobody passed over", sent)
		}
		if sent.PendingApproval.RequestedBy != "dita" || len(sent.PendingApproval.Because) != 1 {
			t.Fatalf("what the route says waits: %+v", sent.PendingApproval)
		}
		if first == uuid.Nil {
			first = sent.PendingApproval.RequestID
		}
		if sent.PendingApproval.RequestID != first {
			t.Fatal("the same apply sent twice made two requests")
		}
	}
	if n := f.requestCount(t); n != 1 {
		t.Fatalf("two applies of one migration left %d request(s), want the one", n)
	}
	f.assertNothingMoved(t, uuidOf(t, v1))

	// Nobody signed in, the route cannot ask — and does not apply instead.
	refused := f.asked(t, f.ctx, skipOpsRequest(v1, v2, false))
	if !errors.Is(refused.Err, apierr.ErrForbidden) || refused.Applied || refused.PendingApproval != nil {
		t.Fatalf("the route's apply with nobody signed in: %+v, want it forbidden", refused)
	}
	f.assertNothingMoved(t, uuidOf(t, v1))
}

// The scope is exact: a cancel, a hold and a plain mapping still apply on one
// administrator's call, and ask nobody (design §9.3).
func TestCancelHoldAndMappingOnlyMigrationsStillApplyOnOneCall(t *testing.T) {
	for _, kind := range []servicecontracts.NodeActionKind{servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold} {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			first, second := f.parkedOnOpsApprove(t)
			v1, v2 := uuidOf(t, first), uuidOf(t, second)
			opts := decideOps(kind, "re-quote")
			plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, opts...)
			if err != nil || !plan.Applicable() || plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 0 {
				t.Fatalf("the plan of a %s: %+v %v, want it to need nobody else", kind, plan, err)
			}
			result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...)
			if err != nil || result.Changed != 1 {
				t.Fatalf("the apply of a %s on one call: %+v %v", kind, result, err)
			}
			instance := f.onlyInstance(t)
			rows := f.ledger(t, instance.ID)
			if len(rows) != 1 || rows[0].RequestID != uuid.Nil || rows[0].ApprovedBy != "" || rows[0].ApprovedByID != uuid.Nil || rows[0].DecidedAt != nil {
				t.Fatalf("the row of a %s nobody else approved: %+v, want it to name no request and no approver", kind, rows)
			}
			if n := f.requestCount(t); n != 0 {
				t.Fatalf("a %s left %d request(s)", kind, n)
			}
		})
	}
	t.Run("a mapping alone", func(t *testing.T) {
		f := newFixture(t)
		v1 := f.deploy(t, "approve")
		if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
		v2 := f.deploy(t, "review")
		mapping := map[string]string{"approve": "review"}
		if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping); err != nil || plan.RequiresSecondApprover {
			t.Fatalf("the plan of a mapping: %+v %v, want it to need nobody else", plan, err)
		}
		if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err != nil {
			t.Fatalf("the apply of a mapping on one call: %v", err)
		}
		if instance := f.onlyInstance(t); instance.Definition == nil || instance.Definition.ID != v2 {
			t.Fatal("the mapping did not move the instance")
		}
		if n := f.requestCount(t); n != 0 {
			t.Fatalf("a mapping left %d request(s)", n)
		}
	})
}

// The twin of "a mapping alone" above: the same kind of migration — a mapping
// and nothing else — in a process with a control the instance has not passed,
// where the mapping sends a step's work to a different step. It asks.
func TestAMappingAloneThatRedirectsPastAControlAsks(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, prepared(f.project, true, true), prepared(f.project, true, true))
	mapping := map[string]string{"prepare": "sign"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover || len(plan.Actions) != 0 || len(plan.ComplianceHolds) != 0 {
		t.Fatalf("the plan of a mapping alone that redirects past a control: %+v %v, want it to need a second administrator with no decision and no hold", plan, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); !errors.Is(err, apierr.ErrForbidden) ||
		!strings.Contains(err.Error(), "a second administrator has to approve it first; nothing was moved") {
		t.Fatalf("the mapping on one administrator's call: %v, want it forbidden", err)
	}
	f.assertWaitingAt(t, v1, "prepare")
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, mapping)
	if err != nil || len(pending.Because) != 1 || !strings.Contains(pending.Because[0], "“Prepare” would be redirected to “Sign”") {
		t.Fatalf("asking for it: %+v %v", pending, err)
	}
	if n := f.requestCount(t); n != 1 {
		t.Fatalf("%d request(s) wait, want the one", n)
	}
	// Its reason is the whole of the stored request's: no step was decided.
	stored, err := f.svc.GetDeviationRequest(adminAs(f.ctx, "dita"), pending.RequestID)
	if err != nil || stored.Reason != "redirected “Prepare” to “Sign”" {
		t.Fatalf("the request's reason is %q (err %v), want it to say what the mapping does", stored.Reason, err)
	}
}

// Rulings §16. Who needs a second administrator is counted from the instances
// that are still running — the only ones a run acts on. A skip over a version
// on which nothing runs asks nobody, though the plan still counts the instance
// that ended there.
func TestASkipOverAVersionWithNothingRunningAsksNobody(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	f.completeTaskOn(t, "opsApprove", "ollie")
	f.completeTaskOn(t, "salesApprove", "sasha")
	if instance := f.onlyInstance(t); instance.Status != entities.ProcessCompleted {
		t.Fatalf("the instance is %s, want it finished on v1", instance.Status)
	}
	opts := skipOps("the role was eliminated")
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, opts...)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: %+v %v", plan, err)
	}
	if plan.Instances != 1 {
		t.Fatalf("the plan counts %d instance(s); it has always counted the one that ended, and still does", plan.Instances)
	}
	if plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 0 {
		t.Fatalf("a skip with nothing running needs a second administrator: %v", plan.SecondApproverReasons)
	}
	if _, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, opts...); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "needs no second administrator") {
		t.Fatalf("asking for a second administrator for it: %v, want it told none is needed", err)
	}
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...)
	if err != nil || result.Changed != 0 {
		t.Fatalf("the apply, on one administrator's call: %+v %v, want it to pass the gate and change nothing", result, err)
	}
	if n := f.requestCount(t); n != 0 {
		t.Fatalf("%d request(s) were made, want none", n)
	}
}

// Rulings §14 (Ruling 32). "Approved" is never where a request rests, and the
// bound on an approved run does not rest on a sweep having run. A process
// that died between approving and reporting leaves the request approved; once
// its window has closed — its own deadline, or an hour after the approval,
// whichever comes first — no apply may run under it, and every reader is told
// it was interrupted. No sweep runs anywhere in this test.
func TestAnApprovedRequestPastItsWindowIsRefusedByTheGateWithNoSweep(t *testing.T) {
	for name, left := range map[string]string{
		// Approved ten minutes ago — inside the hour a run is given — with the
		// request's own deadline since passed.
		"past its own deadline": `decided_at = now() - interval '10 minutes', expires_at = now() - interval '1 minute'`,
		// Approved two hours ago, its deadline still days away.
		"an hour after the approval": `decided_at = now() - interval '2 hours'`,
		// Approved at no recorded time: a window that starts at no known
		// moment is not open.
		"approved at no recorded time": `decided_at = NULL`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			first, second := f.parkedOnOpsApprove(t)
			v1, v2 := uuidOf(t, first), uuidOf(t, second)
			opts := skipOps("the role was eliminated")
			dita := adminAs(f.ctx, "dita")
			pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
			if err != nil {
				t.Fatalf("ask: %v", err)
			}
			// What a stop between the approval and the run's report leaves.
			if err := f.db.Exec(`UPDATE deviation_requests SET status = 'approved', decided_by = 'omar', decided_by_id = ?, `+left+` WHERE id = ?`,
				accountOf("omar"), pending.RequestID).Error; err != nil {
				t.Fatalf("leave the request approved: %v", err)
			}

			under := append(slices.Clip(opts), servicecontracts.WithApprovedRequest(pending.RequestID))
			for range 2 {
				_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
				if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "no longer in use; nothing was moved") {
					t.Fatalf("an apply under an approved request whose window has closed: %v, want it refused", err)
				}
				// Nothing is said of a run that may never have started.
				if strings.Contains(err.Error(), "its run did not report back") {
					t.Fatalf("the refusal says a run did not report back, of a request no run may ever have started under: %v", err)
				}
				f.assertNothingMoved(t, v1)
			}
			if stored := f.storedStatus(t, pending.RequestID); stored != "approved" {
				t.Fatalf("the refused apply left the row %s; the gate only verifies, and writes nothing", stored)
			}

			read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
			if err != nil || read.Status != entities.DeviationRequestInterrupted {
				t.Fatalf("with no sweep having run it reads %q (err %v), want interrupted", read.Status, err)
			}
			for status, want := range map[entities.DeviationRequestStatus]int64{entities.DeviationRequestApproved: 0, entities.DeviationRequestInterrupted: 1} {
				if _, total, err := f.svc.ListDeviationRequests(dita, entities.DeviationRequestQuery{Status: status}); err != nil || total != want {
					t.Fatalf("listed as %s: %d (err %v), want %d", status, total, err, want)
				}
			}
		})
	}
}

// Who needs a second administrator is counted over every instance that has
// not ended, a suspended one among them: it can be made active again between
// the plan's listing and the run's, and must not then be acted on uncounted.
// So a skip over a version whose only instance is suspended still asks — and
// the request shows that instance. The run acts only on instances that are
// active when it reaches them, so it leaves a suspended one where it is: the
// count and the act differ, in the direction that asks.
//
// Nothing in the product suspends an instance, so the state is written
// directly; the instance is otherwise one the product made.
func TestASkipOverAVersionWhoseOnlyInstanceIsSuspendedStillAsks(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	instance := f.onlyInstance(t)
	if err := f.db.Exec(`UPDATE process_instances SET status = 'suspended' WHERE id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("suspend the instance: %v", err)
	}
	opts := skipOps("the role was eliminated")
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, opts...)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 1 {
		t.Fatalf("the plan over a suspended instance: %+v %v, want it to need a second administrator", plan, err)
	}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the apply on one administrator's call: %v, want it forbidden", err)
	}
	dita := adminAs(f.ctx, "dita")
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	stored, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
	if err != nil || !slices.Equal(stored.ApprovedInstances, []uuid.UUID{instance.ID}) {
		t.Fatalf("the request covers %v (err %v), want the suspended instance %s", stored.ApprovedInstances, err, instance.ID)
	}
	// Approved, the run reaches an instance that is not active and leaves it.
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if err != nil || out.MigrationResult == nil || out.MigrationResult.Changed != 0 || out.Request.Status != entities.DeviationRequestApplied {
		t.Fatalf("the approved run over a suspended instance: %+v %v, want it to change nothing and be spent", out, err)
	}
	// Applied, of a run that changed nothing, means only that the request is
	// spent: its outcome says so, and why.
	if out.Request.Outcome["note"] != "no instance was active on the version when it ran" {
		t.Fatalf("the spent request's outcome %v, want it to say nothing was active", out.Request.Outcome)
	}
	if _, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "pia"), pending.RequestID, ""); err == nil ||
		!strings.Contains(err.Error(), "and its run changed nothing: no instance was active on the version when it ran. Ask again for what remains.") {
		t.Fatalf("deciding the spent request again: %v, want to be told its run changed nothing", err)
	}
	after := f.onlyInstance(t)
	if after.Definition == nil || after.Definition.ID != v1 || after.Status != entities.ProcessSuspended || len(f.ledger(t, instance.ID)) != 0 {
		t.Fatalf("the run acted on a suspended instance: %s on %v", after.Status, after.Definition)
	}
}

// What an approval covers is checked twice: when it is given, and again by
// the gate, of the plan the run is made from. An instance that arrives on the
// version after the approval — in the instant before the run plans, or any
// time before an in-process apply — was shown to nobody: the gate refuses the
// whole run, nothing moves, and the request says why.
func TestAnInstanceThatArrivesAfterTheApprovalStopsTheRunAtTheGate(t *testing.T) {
	// staged is a quotation parked on the operations approval, with the next
	// version staged so that a new quotation still starts on the old one.
	staged := func(t *testing.T, f *fixture) (v1, v2 uuid.UUID) {
		t.Helper()
		v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
		f.completeTaskOn(t, "supervisorReview", "sam")
		v2, err = f.svc.DeployDefinition(f.ctx, quotationV2(f), false)
		if err != nil {
			t.Fatalf("stage v2: %v", err)
		}
		return v1, v2
	}
	untouched := func(t *testing.T, f *fixture, v1 uuid.UUID) {
		t.Helper()
		on := f.stillOn(t, v1.String())
		if len(on) != 2 {
			t.Fatalf("%d instance(s) are on the old version, want both", len(on))
		}
		for _, instance := range on {
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Fatalf("the refused run left %d ledger row(s) on %s", len(rows), instance.ID)
			}
			f.assertNoMigrationEntries(t, instance.ID)
		}
	}

	t.Run("between the approval and its run", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		v1, v2 := staged(t, f)
		dita := adminAs(f.ctx, "dita")
		pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		// The approval has listed the version's instances to plan again; the
		// newcomer starts before the run lists them.
		var newcomer uuid.UUID
		listing.on, listing.fire = listing.calls+1, func() {
			started, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil)
			if err != nil {
				t.Errorf("start the newcomer: %v", err)
			}
			newcomer = started
		}
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
		reached(t, listing)
		if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "does not cover this migration") ||
			!strings.Contains(err.Error(), newcomer.String()) || out.Applied {
			t.Fatalf("the run, with a newcomer on the version: %+v %v, want it refused at the gate naming the newcomer", out, err)
		}
		read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
		if err != nil || read.Status != entities.DeviationRequestInterrupted || read.DecidedBy != "omar" {
			t.Fatalf("the request reads %q (err %v), want interrupted: it was approved, and its run did not go ahead", read.Status, err)
		}
		said, _ := read.Outcome["error"].(string)
		if !strings.HasPrefix(said, "the run was refused before it moved anything: 1 instance(s) reached the version being migrated from after it was asked for") ||
			!strings.Contains(said, newcomer.String()) || read.Outcome["changed"] != float64(0) {
			t.Fatalf("its outcome %v, want the gate's reason and that nothing was changed", read.Outcome)
		}
		untouched(t, f, v1)
	})

	t.Run("before an apply under the approved request", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := staged(t, f)
		pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		f.leftApproved(t, pending.RequestID, "1 minute")
		newcomer, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil)
		if err != nil {
			t.Fatalf("start the newcomer: %v", err)
		}
		under := append(slices.Clip(skipOps("the role was eliminated")), servicecontracts.WithApprovedRequest(pending.RequestID))
		_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
		if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "does not cover this migration: 1 instance(s) reached") ||
			!strings.Contains(err.Error(), newcomer.String()) {
			t.Fatalf("an apply under the approved request with a newcomer on the version: %v, want it forbidden naming the newcomer", err)
		}
		untouched(t, f, v1)
	})
}

// The approval a run records is the stored request's, never the caller's
// (Ruling 30). An approval a caller writes into the options is overwritten
// with what the gate verified — with nothing, for a migration that needs
// nobody else — so it is recorded nowhere, on no row and in no entry.
func TestAnApprovalACallerWritesIsRecordedNowhere(t *testing.T) {
	forged := func(approval servicecontracts.MigrationApproval) servicecontracts.MigrationOption {
		return func(o *servicecontracts.MigrationOptions) { o.Approval = approval }
	}
	mallory := servicecontracts.MigrationApproval{RequestedBy: "dita", RequestedByID: accountOf("dita"),
		ApprovedBy: "mallory", ApprovedByID: accountOf("mallory"), DecidedAt: time.Now(), SelfApproved: true, Organization: uuid.Must(uuid.NewV7())}

	t.Run("on a migration that needs nobody else", func(t *testing.T) {
		f := newFixture(t)
		first, second := f.parkedOnOpsApprove(t)
		opts := append(slices.Clip(decideOps(servicecontracts.NodeActionCancel, "re-quote")), forged(mallory))
		result, err := f.svc.ApplyInstanceMigration(f.ctx, uuidOf(t, first), uuidOf(t, second), nil, opts...)
		if err != nil || result.Changed != 1 {
			t.Fatalf("a cancel carrying an approval nobody stored: %+v %v, want it applied on the one call", result, err)
		}
		f.requireNoTraceOf(t, "mallory")
	})

	t.Run("beside a request that was really approved", func(t *testing.T) {
		f := newFixture(t)
		first, second := f.parkedOnOpsApprove(t)
		v1, v2 := uuidOf(t, first), uuidOf(t, second)
		pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		f.leftApproved(t, pending.RequestID, "1 minute")
		// The real request's id, and everything else made up.
		made := mallory
		made.RequestID = pending.RequestID
		opts := append(slices.Clip(skipOps("the role was eliminated")), forged(made))
		result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...)
		if err != nil || result.Changed != 1 {
			t.Fatalf("the apply under the approved request: %+v %v", result, err)
		}
		f.requireNoTraceOf(t, "mallory")
		rows := f.ledger(t, f.onlyInstance(t).ID)
		if len(rows) != 1 || rows[0].ApprovedBy != "omar" || rows[0].ApprovedByID != accountOf("omar") || rows[0].RequestID != pending.RequestID {
			t.Fatalf("the skip's row: %+v, want it to name who the stored request says approved", rows)
		}
		if _, said := rows[0].Details["self_approved"]; said {
			t.Fatalf("the row says self_approved, which only the caller's made-up approval said: %v", rows[0].Details)
		}
	})
}

// requireNoTraceOf fails when the only instance's ledger or trail names
// somebody, anywhere.
func (f *fixture) requireNoTraceOf(t *testing.T, name string) {
	t.Helper()
	instance := f.onlyInstance(t)
	for _, row := range f.ledger(t, instance.ID) {
		if row.ApprovedBy == name || row.ApprovedByID == accountOf(name) || row.Actor == name {
			t.Fatalf("a ledger row names %s: %+v", name, row)
		}
	}
	entries, err := f.svc.GetAuditLogs(f.ctx, instance.ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Narrative, name) || entry.Data["approved_by"] == name {
			t.Fatalf("a %s entry names %s: %q %v", entry.Type, name, entry.Narrative, entry.Data)
		}
	}
}

// requestsUnwired is a repository with no store of requests for a second
// administrator: how a server put together without one looks to the services.
type requestsUnwired struct{ repositories.Repository }

func (requestsUnwired) DeviationRequest() repocontracts.DeviationRequestRepository { return nil }

// A server wired without the store of requests cannot ask, approve or verify.
// That is the server's fault and is answered as that everywhere — never as
// something the caller may not do — and nothing is moved: what needs a second
// administrator is still refused, and an apply that names a request does not
// run on the strength of a store that is not there.
func TestAServerWithNoStoreOfRequestsFailsAsTheServersFaultAndMovesNothing(t *testing.T) {
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository { return requestsUnwired{Repository: repo} })
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	opts := skipOps("the role was eliminated")
	theServers := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s on a server with no store of requests went through", what)
		}
		for _, class := range []error{apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden} {
			if errors.Is(err, class) {
				t.Fatalf("%s is answered as %v: %v; the wiring is the server's fault", what, class, err)
			}
		}
		if !strings.Contains(err.Error(), "wired without the store of requests") {
			t.Fatalf("%s does not say what is missing: %v", what, err)
		}
		f.assertNothingMoved(t, v1)
	}
	_, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, opts...)
	theServers("asking for a migration", err)
	under := append(slices.Clip(opts), servicecontracts.WithApprovedRequest(uuid.Must(uuid.NewV7())))
	_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
	theServers("an apply that names a request", err)
	_, err = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), uuid.Must(uuid.NewV7()), "")
	theServers("approving a request", err)
	// With nothing offered, the gate needs no store to refuse.
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a skip on one administrator's call, on a server with no store of requests: %v, want it forbidden", err)
	}
	f.assertNothingMoved(t, v1)
}

// A request is of one kind for good: the kind is written when the request is
// made and nothing changes it. So a waive's request is no approval of a
// migration, waiting or carried out — offered to the gate, it is refused for
// what it is, and nothing is moved. (The three places that are handed a
// request of the wrong kind by their own caller refuse it too; nothing can
// reach them with one, and they are not run by a test.)
func TestAWaivesRequestIsNoApprovalOfAMigration(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	instance := f.onlyInstance(t)
	dita := adminAs(f.ctx, "dita")

	waive := entities.DeviationCommand{InstanceID: instance.ID, Kind: entities.DeviationWaive, NodeID: "opsApprove",
		Reason: "the operations manager is away; the director agreed", DryRun: true}
	preview, err := f.svc.DeviateInstance(dita, waive)
	if err != nil {
		t.Fatalf("preview the waive: %v", err)
	}
	waive.VisitKey, waive.DryRun = preview.Plan.VisitKey, false
	asked, err := f.svc.DeviateInstance(dita, waive)
	if err != nil || asked.PendingApproval == nil {
		t.Fatalf("ask for the waive: %+v %v", asked, err)
	}
	under := append(slices.Clip(skipOps("the role was eliminated")), servicecontracts.WithApprovedRequest(asked.PendingApproval.RequestID))

	_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
	if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "is pending_approval, not an approved migration; nothing was moved") {
		t.Fatalf("a migration under a waive's request that waits: %v, want it forbidden as no approved migration", err)
	}
	if on := f.stillOn(t, v1.String()); len(on) != 1 {
		t.Fatalf("%d instance(s) are on the version being left, want the one: the refused migration moved something", len(on))
	}

	// Approved and carried out, it is a waive that was made, and still no
	// approval of a migration.
	if out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), asked.PendingApproval.RequestID, ""); err != nil || !out.Applied {
		t.Fatalf("omar approves the waive: %+v %v", out.Request, err)
	}
	_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
	if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "is applied, not an approved migration; nothing was moved") {
		t.Fatalf("a migration under a waive's request that was applied: %v, want it forbidden as no approved migration", err)
	}
	if on := f.stillOn(t, v1.String()); len(on) != 1 {
		t.Fatalf("%d instance(s) are on the version being left, want the one: the refused migration moved something", len(on))
	}
}
