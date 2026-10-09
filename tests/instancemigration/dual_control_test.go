package instancemigration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
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
