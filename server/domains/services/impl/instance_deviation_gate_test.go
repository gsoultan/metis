package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A waive needs a second administrator because it is a waive, not because a
// plan says so: the plan's flag is what a person is shown, and a planner that
// one day stopped setting it must not hand a waive back to one administrator.
// Anything else needs one exactly when its plan says it does.
func TestAWaiveNeedsASecondAdministratorWhateverItsPlanSays(t *testing.T) {
	for name, c := range map[string]struct {
		kind    entities.DeviationKind
		flagged bool
		want    bool
	}{
		"a waive whose plan says so":           {entities.DeviationWaive, true, true},
		"a waive whose plan does not":          {entities.DeviationWaive, false, true},
		"a cancel":                             {entities.DeviationCancel, false, false},
		"a hold":                               {entities.DeviationHold, false, false},
		"a cancel whose plan one day says so":  {entities.DeviationCancel, true, true},
		"a hold whose plan one day says so":    {entities.DeviationHold, true, true},
		"a kind nothing knows, flagged or not": {entities.DeviationKind("reroute"), false, false},
	} {
		got := needsSecondAdministrator(entities.DeviationCommand{Kind: c.kind}, entities.DeviationPlan{Kind: c.kind, RequiresSecondApprover: c.flagged})
		if got != c.want {
			t.Errorf("%s: needs a second administrator %v, want %v", name, got, c.want)
		}
	}
}

// The plan of a waive with its flag forced off is still sent to a request,
// never applied. Here the service has no store of requests, so being sent to
// one is refused in that store's words — and nothing else is wired at all:
// had the waive been acted on instead, the engine it would have reached is
// nil. An administrator with no account is refused before that, as whoever
// asks for a second administrator is.
func TestAWaiveWhosePlanSaysNothingIsStillSentToARequest(t *testing.T) {
	organization := uuid.Must(uuid.NewV7())
	as := func(account entities.User) context.Context {
		ctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: organization.String()})
		return context.WithValue(ctx, pkgauth.UserContextKey, account)
	}
	service := &instanceDeviationService{repo: ledgerAndRequests{}}
	locked := rowWaitingAt(models.ProcessActive, "approve")
	live := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	command := entities.DeviationCommand{InstanceID: uuid.UUID(locked.ID), Kind: entities.DeviationWaive, NodeID: "approve", Reason: "the CFO agreed", VisitKey: "dv1-k"}
	plan := entities.DeviationPlan{InstanceID: command.InstanceID, Kind: entities.DeviationWaive, NodeID: "approve", NodeName: "Approve", VisitKey: "dv1-k"}

	ana := entities.User{ID: uuid.Must(uuid.NewV7()), Username: "ana", Roles: []string{entities.RoleAdmin}}
	out, err := service.carryOut(as(ana), locked, &live, plan, command, "ana")
	if !errors.Is(err, errNoDeviationRequests) || out.Applied || out.Deviation != nil {
		t.Fatalf("a waive whose plan does not ask for a second administrator: %+v, %v; want it sent to a request all the same", out, err)
	}
	nameless := entities.User{Username: "ana", Roles: []string{entities.RoleAdmin}}
	if _, err := service.carryOut(as(nameless), locked, &live, plan, command, "ana"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the same waive by an administrator with no account: %v, want forbidden", err)
	}
	// Only a waive has a request to be made for it. Were a plan one day to
	// say a cancel needed somebody else, it is neither acted on nor asked for
	// as though it were a waive: it is refused as the server's mistake.
	cancel, flagged := command, plan
	cancel.Kind, flagged.Kind, flagged.RequiresSecondApprover = entities.DeviationCancel, entities.DeviationCancel, true
	out, err = service.carryOut(as(ana), locked, &live, flagged, cancel, "ana")
	if err == nil || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrForbidden) || errors.Is(err, errNoDeviationRequests) ||
		out.Applied || out.Deviation != nil {
		t.Fatalf("a cancel planned as needing a second administrator: %+v, %v; want it refused as the server's mistake", out, err)
	}
}

// And the act itself has no waive in it: asked for one, it refuses as the
// server's own mistake and reaches nothing. There is no code left that makes
// a waive on the word of whoever asked.
func TestTheActOfOneAdministratorIsNeverAWaive(t *testing.T) {
	service := &instanceDeviationService{}
	locked := rowWaitingAt(models.ProcessActive, "approve")
	live := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	command := entities.DeviationCommand{InstanceID: uuid.UUID(locked.ID), Kind: entities.DeviationWaive, NodeID: "approve", Reason: "the CFO agreed"}
	plan := entities.DeviationPlan{Kind: entities.DeviationWaive, NodeID: "approve", NodeName: "Approve"}
	row, err := service.act(context.Background(), locked, &live, &entities.ProcessDefinition{}, plan, command, "ana")
	if err == nil || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrForbidden) || errors.Is(err, apierr.ErrNotFound) ||
		!strings.Contains(err.Error(), "second administrator") {
		t.Fatalf("a waive handed to the act: %v, want it refused as the server's mistake, saying a waive waits for a second administrator", err)
	}
	if row.ID != uuid.Nil || row.Kind != "" {
		t.Fatalf("the refused act answered a row: %+v", row)
	}
}

// The approval of a waive asks who is approving itself: it does not take its
// caller's word for it. Whoever reaches it without being an administrator of
// the organization, signed in with an account, is refused before anything is
// read — the service here has no repository at all, so reading would panic.
func TestTheApprovalOfAWaiveAsksWhoIsApprovingItself(t *testing.T) {
	organization := uuid.Must(uuid.NewV7())
	in := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: organization.String()})
	service := &instanceDeviationService{}
	for who, ctx := range map[string]context.Context{
		"nobody signed in": in,
		"an operator": context.WithValue(in, pkgauth.UserContextKey,
			entities.User{ID: uuid.Must(uuid.NewV7()), Username: "olga", Roles: []string{entities.RoleOperator}}),
		"an administrator of another organization": context.WithValue(in, pkgauth.UserContextKey, entities.User{ID: uuid.Must(uuid.NewV7()), Username: "otto",
			RolesByOrganization: map[uuid.UUID][]string{uuid.Must(uuid.NewV7()): {entities.RoleAdmin}}}),
		"an administrator with no account": context.WithValue(in, pkgauth.UserContextKey,
			entities.User{Username: "budi", Roles: []string{entities.RoleAdmin}}),
		"an administrator asking for no organization": context.WithValue(context.Background(), pkgauth.UserContextKey,
			entities.User{ID: uuid.Must(uuid.NewV7()), Username: "budi", Roles: []string{entities.RoleAdmin}}),
	} {
		out, err := service.approveWaive(ctx, uuid.Must(uuid.NewV7()), "let it through")
		if !errors.Is(err, apierr.ErrForbidden) || out.Applied || out.Deviation != nil {
			t.Errorf("%s reaching the approval of a waive: %+v, %v; want forbidden", who, out, err)
		}
	}
}
