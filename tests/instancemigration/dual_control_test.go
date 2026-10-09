package instancemigration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
)

// A migration that skips a step is asked for, not made: the ask writes one
// request that waits, shows what was asked and which instances it covers,
// and changes nothing else.
func TestAskingForAMigrationWritesARequestAndMovesNothing(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	opts := skipOps("the role was eliminated")
	dita := adminAs(f.ctx, "dita")

	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil || pending.RequestID == uuid.Nil || pending.Status != entities.DeviationRequestPending || pending.RequestedBy != "dita" {
		t.Fatalf("the ask: %+v %v", pending, err)
	}
	if len(pending.Because) != 1 || !strings.Contains(pending.Because[0], "“Operations approve” would be skipped for every listed instance waiting at it when the migration runs") {
		t.Fatalf("why it needs somebody else: %v", pending.Because)
	}
	f.assertNothingMoved(t, v1)
	if n := f.requestCount(t); n != 1 {
		t.Fatalf("%d request(s) were written, want the one", n)
	}

	instance := f.onlyInstance(t)
	stored, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
	if err != nil {
		t.Fatalf("read the request: %v", err)
	}
	if stored.Kind != entities.DeviationRequestMigration || stored.Instance != nil || stored.RequestedByID != accountOf("dita") ||
		stored.SourceDefinition == nil || stored.SourceDefinition.ID != v1 || stored.TargetDefinition == nil || stored.TargetDefinition.ID != v2 ||
		stored.Project == nil || stored.Project.ID != f.project {
		t.Fatalf("the request as stored: %+v", stored)
	}
	if !strings.HasPrefix(stored.Fingerprint, "mf1-") || stored.Reason != "the role was eliminated" {
		t.Fatalf("its fingerprint %q and reason %q", stored.Fingerprint, stored.Reason)
	}
	if !slices.Equal(stored.ApprovedInstances, []uuid.UUID{instance.ID}) {
		t.Fatalf("it covers %v, want the one running instance %s", stored.ApprovedInstances, instance.ID)
	}
	actions, _ := stored.Command["node_actions"].(map[string]any)
	skip, _ := actions["opsApprove"].(map[string]any)
	if stored.Command["source_definition_id"] != first || stored.Command["target_definition_id"] != second ||
		skip["kind"] != "skip" || skip["reason"] != "the role was eliminated" {
		t.Fatalf("the command it stores: %v", stored.Command)
	}
	if stored.Plan["requires_second_approver"] != true || !slices.Equal(stored.Because(), pending.Because) {
		t.Fatalf("the plan it stores: %v", stored.Plan)
	}
	if !stored.ExpiresAt.After(stored.CreatedAt) || stored.DecidedBy != "" || stored.DecidedAt != nil {
		t.Fatalf("its deadline %s and decision %q: want a deadline ahead and nobody having decided", stored.ExpiresAt, stored.DecidedBy)
	}

	// Asked again by whoever asked, it is the same request.
	again, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil || again.RequestID != pending.RequestID || again.Status != entities.DeviationRequestPending {
		t.Fatalf("the same ask again: %+v %v, want the request that waits", again, err)
	}
	// Asked by somebody else, it is refused and they are pointed at it: a
	// second administrator does not get a request of their own to approve.
	_, err = f.svc.RequestMigrationApproval(adminAs(f.ctx, "omar"), v1, v2, nil, opts...)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "already waiting for approval") ||
		!strings.Contains(err.Error(), pending.RequestID.String()) || !strings.Contains(err.Error(), "asked by dita") {
		t.Fatalf("the same migration asked for by another administrator: %v", err)
	}
	if n := f.requestCount(t); n != 1 {
		t.Fatalf("asking again left %d request(s), want the one", n)
	}
	f.assertNothingMoved(t, v1)
}

// Only a signed-in administrator of the organization, with an account, asks.
// Anybody else is refused before anything is planned or written.
func TestOnlyAnAdministratorWithAnAccountAsksForAMigration(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	opts := skipOps("the role was eliminated")
	as := func(user entities.User) error {
		_, err := f.svc.RequestMigrationApproval(signedInAs(f, user), v1, v2, nil, opts...)
		return err
	}
	for name, err := range map[string]error{
		"nobody signed in":                 func() error { _, err := f.svc.RequestMigrationApproval(f.ctx, v1, v2, nil, opts...); return err }(),
		"a user who is no administrator":   as(entities.User{ID: accountOf("uma"), Username: "uma", Roles: []string{entities.RoleUser}}),
		"an administrator with no account": as(entities.User{Username: "dita", Roles: []string{entities.RoleAdmin}}),
		"an administrator with no name":    as(entities.User{ID: accountOf("dita"), Roles: []string{entities.RoleAdmin}}),
	} {
		if !errors.Is(err, apierr.ErrForbidden) {
			t.Errorf("%s: %v, want it forbidden", name, err)
		}
	}
	if n := f.requestCount(t); n != 0 {
		t.Fatalf("refused asks left %d request(s)", n)
	}
	f.assertNothingMoved(t, v1)
}

// signedInAs is the fixture's context with user signed in.
func signedInAs(f *fixture, user entities.User) context.Context {
	return context.WithValue(f.ctx, pkgauth.UserContextKey, user)
}

// What needs nobody else is not asked for, and what the plan refuses is
// refused: neither leaves a request.
func TestAMigrationThatNeedsNobodyElseOrIsRefusedIsNotAskedFor(t *testing.T) {
	dita := func(f *fixture) context.Context { return adminAs(f.ctx, "dita") }
	t.Run("a mapping alone", func(t *testing.T) {
		f := newFixture(t)
		v1 := f.deploy(t, "approve")
		if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
		v2 := f.deploy(t, "review")
		_, err := f.svc.RequestMigrationApproval(dita(f), v1, v2, map[string]string{"approve": "review"})
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "needs no second administrator") {
			t.Fatalf("asking for a migration that only maps: %v", err)
		}
		if n := f.requestCount(t); n != 0 {
			t.Fatalf("it left %d request(s)", n)
		}
	})
	t.Run("a cancel", func(t *testing.T) {
		f := newFixture(t)
		first, second := f.parkedOnOpsApprove(t)
		_, err := f.svc.RequestMigrationApproval(dita(f), uuidOf(t, first), uuidOf(t, second), nil,
			decideOps(servicecontracts.NodeActionCancel, "re-quote")...)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "needs no second administrator") {
			t.Fatalf("asking for a migration that cancels: %v", err)
		}
		if n := f.requestCount(t); n != 0 {
			t.Fatalf("it left %d request(s)", n)
		}
	})
	t.Run("a plan that refuses", func(t *testing.T) {
		f := newFixture(t)
		first, second := f.parkedOnOpsApprove(t)
		// No reason: the planner refuses a skip that does not say why.
		_, err := f.svc.RequestMigrationApproval(dita(f), uuidOf(t, first), uuidOf(t, second), nil, skipOps("")...)
		if !errors.Is(err, apierr.ErrInvalidArgument) || strings.Contains(err.Error(), "needs no second administrator") {
			t.Fatalf("asking for a migration the plan refuses: %v, want the plan's refusal", err)
		}
		if n := f.requestCount(t); n != 0 {
			t.Fatalf("it left %d request(s)", n)
		}
		f.assertNothingMoved(t, uuidOf(t, first))
	})
}

// B2, B6: a skip, and an acknowledged control loss, wait for a second
// administrator; approved, they run, and every row names both people.
func TestASkipOrAnAcknowledgedHoldWaitsForASecondAdministrator(t *testing.T) {
	cases := map[string]func(t *testing.T, f *fixture) (v1, v2 uuid.UUID, mapping map[string]string, opts []servicecontracts.MigrationOption){
		"a skip": func(t *testing.T, f *fixture) (uuid.UUID, uuid.UUID, map[string]string, []servicecontracts.MigrationOption) {
			a, b := f.parkedOnOpsApprove(t)
			return uuidOf(t, a), uuidOf(t, b), nil, []servicecontracts.MigrationOption{servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
				"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the operations manager role was eliminated"}})}
		},
		"an acknowledged hold": func(t *testing.T, f *fixture) (uuid.UUID, uuid.UUID, map[string]string, []servicecontracts.MigrationOption) {
			v1, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "opsApprove", true))
			if err != nil {
				t.Fatalf("deploy v1: %v", err)
			}
			if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-approval", nil); err != nil {
				t.Fatalf("start: %v", err)
			}
			v2, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "salesApprove", false))
			if err != nil {
				t.Fatalf("deploy v2: %v", err)
			}
			return v1, v2, map[string]string{"opsApprove": "salesApprove"}, []servicecontracts.MigrationOption{servicecontracts.WithAcknowledgedHolds("opsApprove")}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			v1, v2, mapping, opts := build(t, f)
			plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, opts...)
			if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 1 {
				t.Fatalf("the dry run: %+v %v", plan, err)
			}
			pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, mapping, opts...)
			if err != nil || pending.Status != entities.DeviationRequestPending || pending.RequestedBy != "dita" || len(pending.Because) != 1 {
				t.Fatalf("the request: %+v %v", pending, err)
			}
			instance := f.onlyInstance(t)
			if instance.Definition == nil || instance.Definition.ID != v1 {
				t.Fatal("asking for approval moved the instance")
			}
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Fatalf("a pending migration wrote %d ledger row(s)", len(rows))
			}
			out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "agreed at the change board")
			if err != nil || !out.Applied || out.Request.Status != entities.DeviationRequestApplied || out.MigrationPlan == nil ||
				out.MigrationResult == nil || out.MigrationResult.Changed != 1 || len(out.MigrationResult.PassedOver) != 0 {
				t.Fatalf("omar approves: %+v %v", out, err)
			}
			if out.Deviation != nil || out.WaivePlan != nil {
				t.Fatalf("an approved migration answered a waive's record: %+v", out)
			}
			rows := f.ledger(t, instance.ID)
			if len(rows) == 0 {
				t.Fatal("the approved run wrote no ledger row")
			}
			for _, row := range rows {
				if row.RequestID != pending.RequestID || row.ApprovedBy != "omar" || row.Actor != "dita" || row.ActorID == uuid.Nil {
					t.Fatalf("a row of the approved run: %+v", row)
				}
				if row.ApprovedByID != accountOf("omar") || row.ActorID != accountOf("dita") || row.DecidedAt == nil || row.Status != entities.DeviationApplied {
					t.Fatalf("a row of the approved run names accounts %s and %s, decided %v, status %s", row.ActorID, row.ApprovedByID, row.DecidedAt, row.Status)
				}
				// Somebody else approved: the row says nothing of a
				// self-approval, not that there was none.
				for _, key := range []string{"self_approved", "other_administrators", "organization_id"} {
					if _, said := row.Details[key]; said {
						t.Fatalf("a row a second administrator approved carries %q: %v", key, row.Details)
					}
				}
			}
			entries, err := f.svc.GetAuditLogs(f.ctx, instance.ID)
			if err != nil {
				t.Fatalf("read the trail: %v", err)
			}
			named := 0
			for _, entry := range entries {
				if entry.Type != impl.EventNodeSkipped && entry.Type != impl.EventInstanceMigrated {
					continue
				}
				if entry.Data["approved_by"] != "omar" || entry.Data["request_id"] != pending.RequestID.String() ||
					!strings.Contains(entry.Narrative, "dita") ||
					!strings.HasSuffix(entry.Narrative, " A second administrator, omar, approved this migration (request "+pending.RequestID.String()+").") {
					t.Fatalf("an entry of the approved run: %q %v", entry.Narrative, entry.Data)
				}
				if _, said := entry.Data["self_approved"]; said {
					t.Fatalf("an entry a second administrator approved carries self_approved: %v", entry.Data)
				}
				named++
			}
			if named == 0 {
				t.Fatal("no entry of the run names the requester, the approver and the request")
			}
			// The request says who decided and what the run did, and holds
			// nothing any longer.
			read, err := f.svc.GetDeviationRequest(adminAs(f.ctx, "dita"), pending.RequestID)
			if err != nil || read.Status != entities.DeviationRequestApplied || read.DecidedBy != "omar" || read.DecidedByID != accountOf("omar") ||
				read.DecisionReason != "agreed at the change board" || read.SelfApproved() {
				t.Fatalf("the request after the run: %+v %v", read, err)
			}
			if read.Outcome["changed"] != float64(1) || read.Outcome["passed_over"] != float64(0) || len(read.Outcome) != 2 {
				t.Fatalf("its outcome %v, want what the run did and nothing else", read.Outcome)
			}
			if f.storedStatus(t, pending.RequestID) != "applied" {
				t.Fatal("the request's row does not say applied")
			}
		})
	}
}

// B3: dita cannot approve her own migration — by account, so not under
// another name either — and the attempt changes nothing.
func TestTheRequesterNeverApprovesTheirOwnMigration(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	renamed := signedInAs(f, entities.User{ID: accountOf("dita"), Username: "dita-the-second", Roles: []string{entities.RoleAdmin}})
	for name, ctx := range map[string]context.Context{"as herself": adminAs(f.ctx, "dita"), "under another name": renamed} {
		out, err := f.svc.ApproveDeviationRequest(ctx, pending.RequestID, "I am sure")
		if !errors.Is(err, apierr.ErrForbidden) || out.Applied || out.MigrationResult != nil {
			t.Fatalf("%s: %+v %v, want it forbidden", name, out, err)
		}
		if stored := f.storedStatus(t, pending.RequestID); stored != "pending_approval" {
			t.Fatalf("%s: the request is %s, want it still waiting", name, stored)
		}
		f.assertNothingMoved(t, v1)
	}
	// And somebody who is not an administrator decides nothing.
	uma := signedInAs(f, entities.User{ID: accountOf("uma"), Username: "uma", Roles: []string{entities.RoleUser}})
	if _, err := f.svc.ApproveDeviationRequest(uma, pending.RequestID, ""); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a user who is no administrator approves: %v, want it forbidden", err)
	}
	f.assertNothingMoved(t, v1)
}

// Rulings addendum §12. An approved run reports as any run does: the instance
// whose holder completed the step after the run listed it is passed over, and
// the approval's answer names it and says why — it is not swallowed because the
// run was somebody else's to approve.
func TestAnApprovedRunStillSaysWhichInstancesItPassedOver(t *testing.T) {
	f, listing := newRacedFixture(t)
	first, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	for range 2 {
		if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
		f.completeTaskOn(t, "supervisorReview", "sam")
	}
	second, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	opts := skipOps("the role was eliminated")
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), first, second, nil, opts...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	listing.atTheApprovedApplysListing(func() { f.completeTaskOn(t, "opsApprove", "ollie") })
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	reached(t, listing)
	if err != nil || !out.Applied || out.MigrationResult == nil || out.MigrationResult.Changed != 1 || len(out.MigrationResult.PassedOver) != 1 {
		t.Fatalf("the approved run: %+v %v, want one instance moved and one passed over", out, err)
	}
	left := f.stillOn(t, first.String())
	if len(left) != 1 || len(f.stillOn(t, second.String())) != 1 {
		t.Fatalf("%d instance(s) are on v1, want the one passed over", len(left))
	}
	passed := out.MigrationResult.PassedOver[0]
	if passed.Instance == nil || passed.Instance.ID != left[0].ID || !strings.Contains(passed.Reason, "Operations approve") {
		t.Fatalf("passed over: %+v, want the instance still on v1 and why", passed)
	}
	if out.Request.Status != entities.DeviationRequestApplied || out.Request.Outcome["passed_over"] != float64(1) || out.Request.Outcome["changed"] != float64(1) {
		t.Fatalf("the request: %q %v, want applied with one moved and one passed over", out.Request.Status, out.Request.Outcome)
	}
	if rows := f.ledger(t, left[0].ID); len(rows) != 0 {
		t.Errorf("the instance passed over has %d ledger row(s)", len(rows))
	}
}

// B4: a request approves the instances it showed. One that joined since makes
// it stale; one that left does not.
func TestANewcomerMakesAMigrationRequestStaleAndALeaverDoesNot(t *testing.T) {
	t.Run("newcomer", func(t *testing.T) {
		f := newFixture(t)
		v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
		f.completeTaskOn(t, "supervisorReview", "sam")
		listed := f.onlyInstance(t)
		// Staged: version 1 stays the one a new quotation starts on.
		v2, err := f.svc.DeployDefinition(f.ctx, quotationV2(f), false)
		if err != nil {
			t.Fatalf("stage v2: %v", err)
		}
		dita := adminAs(f.ctx, "dita")
		pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		newcomer, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil)
		if err != nil {
			t.Fatalf("start the newcomer: %v", err)
		}
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "no longer holds") ||
			!strings.Contains(err.Error(), newcomer.String()) || strings.Contains(err.Error(), listed.ID.String()) || out.Applied {
			t.Fatalf("approving with a newcomer on the version: %+v %v, want it refused naming the newcomer", out, err)
		}
		read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
		if err != nil || read.Status != entities.DeviationRequestStale || f.storedStatus(t, pending.RequestID) != "stale" {
			t.Fatalf("the request reads %q (err %v), want stale and kept so", read.Status, err)
		}
		if refusals, _ := read.Outcome["refusals"].([]any); len(refusals) != 1 || !strings.Contains(refusals[0].(string), newcomer.String()) {
			t.Fatalf("its outcome %v, want the newcomer named among the refusals", read.Outcome)
		}
		if read.DecidedBy != "" || read.DecidedByID != uuid.Nil || read.SelfApproved() {
			t.Fatalf("a stale request names %q as having decided it; the version did", read.DecidedBy)
		}
		if on := f.stillOn(t, v1.String()); len(on) != 2 {
			t.Fatalf("%d instance(s) are on v1, want both", len(on))
		}
		for _, instance := range f.stillOn(t, v1.String()) {
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Fatalf("a stale request left %d ledger row(s) on %s", len(rows), instance.ID)
			}
			f.assertNoMigrationEntries(t, instance.ID)
		}
		// The same thing can be asked for afresh, and then covers both.
		again, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil || again.RequestID == pending.RequestID {
			t.Fatalf("asking afresh after a stale request: %+v %v, want a new request", again, err)
		}
	})

	t.Run("leaver", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.severalParkedOnOpsApprove(t, 2)
		pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		f.completeTaskOn(t, "opsApprove", "ollie")
		f.completeTaskOn(t, "salesApprove", "sasha")
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
		if err != nil || !out.Applied || out.Request.Status != entities.DeviationRequestApplied || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
			t.Fatalf("approving after one instance finished: %+v %v, want it applied to the one that remains", out, err)
		}
		if on := f.stillOn(t, v2.String()); len(on) != 1 || on[0].Status != entities.ProcessActive {
			t.Fatalf("%d running instance(s) are on v2, want the one that was still waiting", len(on))
		}
	})
}

// B6: the run stops part-way; the request says so and what the run reached is
// recorded — in a sentence of the server's own, not the failure's words.
func TestARunThatFailsPartWayLeavesTheRequestInterrupted(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, routed(f, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "claim", nil); err != nil {
		t.Fatalf("start a claim: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, routed(f, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	opts := []servicecontracts.MigrationOption{servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
		"review": {Kind: servicecontracts.NodeActionSkip, Reason: "claims under 50 are no longer reviewed"}})}
	dita := adminAs(f.ctx, "dita")
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if err == nil || out.Applied || out.Request.Status != entities.DeviationRequestInterrupted || out.MigrationResult == nil {
		t.Fatalf("approving a skip whose advance cannot route: %+v %v, want the failure beside a request left interrupted", out, err)
	}
	if !strings.Contains(err.Error(), "interrupted") || !strings.Contains(err.Error(), pending.RequestID.String()) {
		t.Fatalf("the failure %q does not say what became of the request", err)
	}
	read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
	if err != nil || read.Status != entities.DeviationRequestInterrupted || read.DecidedBy != "omar" {
		t.Fatalf("the request reads %q decided by %q (err %v), want interrupted, approved by omar", read.Status, read.DecidedBy, err)
	}
	said, _ := read.Outcome["error"].(string)
	if said != "the run stopped on a failure after it had acted on 0 instance(s); what it had done by then stands" ||
		read.Outcome["changed"] != float64(0) || read.Outcome["passed_over"] != float64(0) {
		t.Fatalf("its outcome %v, want the server's own sentence and what the run reached", read.Outcome)
	}
	if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != "review" {
		t.Fatalf("%d task(s) are open (%+v), want the review still to be done", len(open), open)
	}
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 || len(f.ledger(t, instance.ID)) != 0 {
		t.Fatal("the failed skip moved the instance or left a ledger row")
	}
	// It holds nothing any longer: the same thing can be asked for again.
	if again, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...); err != nil || again.RequestID == pending.RequestID {
		t.Fatalf("asking again after an interrupted run: %+v %v, want a new request", again, err)
	}
	// And nobody decides it a second time.
	if _, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, ""); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "the run stopped part-way") {
		t.Fatalf("approving an interrupted request again: %v", err)
	}
}

// B5: a request nobody decided before its deadline cannot be approved. The
// approval that finds it so records the expiry — and keeps the record — and
// is refused; nothing moves.
func TestAnExpiredMigrationRequestCannotBeApproved(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	dita := adminAs(f.ctx, "dita")
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	f.letTimePass(t, pending.RequestID, 1)
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "expired on") || out.Applied {
		t.Fatalf("approving past the deadline: %+v %v, want it refused as expired", out, err)
	}
	if stored := f.storedStatus(t, pending.RequestID); stored != "expired" {
		t.Fatalf("the request's row says %s, want the expiry kept", stored)
	}
	f.assertNothingMoved(t, v1)
	// The requester is refused as the requester, and records nothing: a
	// refused attempt of theirs never changes state.
	other, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("another reason")...)
	if err != nil {
		t.Fatalf("ask for another: %v", err)
	}
	f.letTimePass(t, other.RequestID, 1)
	if _, err := f.svc.ApproveDeviationRequest(dita, other.RequestID, "mine"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the requester approving their own overdue request: %v, want it forbidden", err)
	}
	if stored := f.storedStatus(t, other.RequestID); stored != "pending_approval" {
		t.Fatalf("the requester's refused attempt left the row %s, want it untouched", stored)
	}
}

// A request is decided once: approved and run, it is not approved again, and
// the second attempt moves nothing.
func TestAMigrationRequestIsApprovedOnce(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.severalParkedOnOpsApprove(t, 2)
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, ""); err != nil || !out.Applied || out.MigrationResult.Changed != 2 {
		t.Fatalf("the approval: %+v %v", out, err)
	}
	var rows int
	for _, instance := range f.stillOn(t, v2.String()) {
		rows += len(f.ledger(t, instance.ID))
	}
	for _, name := range []string{"omar", "pia"} {
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, name), pending.RequestID, "")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "omar approved this") ||
			!strings.Contains(err.Error(), "and it was applied") || out.Applied {
			t.Fatalf("%s approves it again: %+v %v, want to be told it was approved and applied", name, out, err)
		}
	}
	var after int
	for _, instance := range f.stillOn(t, v2.String()) {
		after += len(f.ledger(t, instance.ID))
	}
	if rows != 2 || after != rows {
		t.Fatalf("the ledger held %d row(s) after the run and %d after the second attempts, want two both times", rows, after)
	}
}

// B6, with something done before the failure. A run that fails after acting
// on one instance leaves the request interrupted, saying it had acted on one;
// what it did stands and names that request; and what remains is asked for
// again and finished under the second request — never under the first.
func TestARunThatFailsAfterActingOnOneInstanceIsFinishedUnderAnotherRequest(t *testing.T) {
	writes := &atomic.Int32{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return ledgerFailingOnceRepository{Repository: repo, writes: writes, failOn: 2}
	})
	v1, v2 := f.severalParkedOnOpsApprove(t, 3)
	opts := skipOps("the role was eliminated")
	dita := adminAs(f.ctx, "dita")
	first, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), first.RequestID, "")
	if err == nil || out.Applied || out.Request.Status != entities.DeviationRequestInterrupted || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
		t.Fatalf("a run whose second skip could not be recorded: %+v %v, want it to stop having acted on one", out, err)
	}
	// One thing to do about it is said, and it is the true one: ask again.
	if said := err.Error(); !strings.Contains(said, "what remains has to be asked for again") || strings.Contains(said, "run the same migration again") ||
		!strings.Contains(said, "1 of 3 instances had already been dealt with") || !strings.Contains(said, "the ledger lost its connection") {
		t.Fatalf("the failure says %q; want how far the run got, why it stopped, and only that what remains has to be asked for again", said)
	}
	read, err := f.svc.GetDeviationRequest(dita, first.RequestID)
	if err != nil || read.Status != entities.DeviationRequestInterrupted || read.Outcome["changed"] != float64(1) || read.Outcome["passed_over"] != float64(0) ||
		read.Outcome["error"] != "the run stopped on a failure after it had acted on 1 instance(s); what it had done by then stands" {
		t.Fatalf("the first request reads %q with %v (err %v), want interrupted, saying one instance had been acted on", read.Status, read.Outcome, err)
	}
	if strings.Contains(fmt.Sprint(read.Outcome), "the ledger lost its connection") {
		t.Fatalf("the request's outcome carries the failure's own words: %v", read.Outcome)
	}
	done := f.stillOn(t, v2.String())
	if len(done) != 1 || len(f.stillOn(t, v1.String())) != 2 {
		t.Fatalf("%d instance(s) moved, want the one the run reached before it failed", len(done))
	}
	if rows := f.ledger(t, done[0].ID); len(rows) != 1 || rows[0].RequestID != first.RequestID || rows[0].ApprovedBy != "omar" {
		t.Fatalf("the first instance's ledger: %+v, want its skip under the first request", rows)
	}
	for _, left := range f.stillOn(t, v1.String()) {
		f.assertUntouched(t, left, v1)
	}

	// Asked again, it is another request, for the two that remain.
	second, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, opts...)
	if err != nil || second.RequestID == first.RequestID {
		t.Fatalf("asking again for what remains: %+v %v, want a second request", second, err)
	}
	rest, err := f.svc.GetDeviationRequest(dita, second.RequestID)
	if err != nil || len(rest.ApprovedInstances) != 2 || slices.Contains(rest.ApprovedInstances, done[0].ID) {
		t.Fatalf("the second request covers %v (err %v), want the two instances the first run did not reach", rest.ApprovedInstances, err)
	}
	out, err = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "pia"), second.RequestID, "")
	if err != nil || !out.Applied || out.MigrationResult.Changed != 2 || out.Request.Status != entities.DeviationRequestApplied {
		t.Fatalf("the second request's run: %+v %v, want it to finish the two that remained", out, err)
	}
	for _, instance := range f.stillOn(t, v2.String()) {
		rows := f.ledger(t, instance.ID)
		want, approver := second.RequestID, "pia"
		if instance.ID == done[0].ID {
			want, approver = first.RequestID, "omar"
		}
		if len(rows) != 1 || rows[0].RequestID != want || rows[0].ApprovedBy != approver {
			t.Fatalf("instance %s: %+v, want one skip under request %s approved by %s", instance.ID, rows, want, approver)
		}
	}
	if got := f.storedStatus(t, first.RequestID); got != "interrupted" {
		t.Fatalf("the first request is %s after the second ran, want it left interrupted", got)
	}
}

// A run that panics does not leave its request approved for an hour. The
// request is closed as interrupted before the panic goes on its way, saying
// that the server failed and that how far the run got is not known — nothing
// is claimed about a count nobody has.
func TestARunThatPanicsLeavesItsRequestInterruptedAndNotApproved(t *testing.T) {
	f, listing := newRacedFixture(t)
	dita, v1, v2, requestID := f.askToSkipOps(t)
	listing.atTheApprovedApplysListing(func() { panic("the listing fell over") })

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, _ = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
	}()
	if panicked == nil || !strings.Contains(fmt.Sprint(panicked), "the listing fell over") {
		t.Fatalf("the approval recovered from the run's panic and went on as if nothing had happened: %v", panicked)
	}
	read, err := f.svc.GetDeviationRequest(dita, requestID)
	if err != nil || f.storedStatus(t, requestID) != "interrupted" || read.DecidedBy != "omar" {
		t.Fatalf("the request is stored %s (err %v), want it interrupted and not left approved", f.storedStatus(t, requestID), err)
	}
	if read.Outcome["error"] != "the run stopped on a failure of the server's own, and how far it had got is not known; what it had done by then stands" {
		t.Fatalf("its outcome %v, want it to say the server failed and that the count is not known", read.Outcome)
	}
	if _, counted := read.Outcome["changed"]; counted {
		t.Fatalf("its outcome claims a count nobody has: %v", read.Outcome)
	}
	if strings.Contains(fmt.Sprint(read.Outcome), "the listing fell over") {
		t.Fatalf("the outcome carries the panic's own words: %v", read.Outcome)
	}
	f.assertNothingMoved(t, v1)
	// It holds nothing: the same migration is asked for again at once.
	if again, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...); err != nil || again.RequestID == requestID {
		t.Fatalf("asking again after a run that panicked: %+v %v, want a fresh request", again, err)
	}
}
