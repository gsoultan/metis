package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The rule that decides a hand-over is asked of the row the service holds, so
// it has to be answerable from the task and the request's context alone.

func signedInAs(username string, roles ...string) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.User{Username: username, Roles: roles})
}

func heldBy(username string) entities.Task {
	return entities.Task{Status: entities.TaskClaimed, Assignee: &entities.User{Username: username}}
}

func TestWhoMayHandATaskOn(t *testing.T) {
	t.Parallel()
	offered := entities.Task{Status: entities.TaskUnclaimed, CandidateGroups: []*entities.Group{{Name: "finance"}}}
	unnamed := entities.Task{Status: entities.TaskUnclaimed}

	cases := []struct {
		name    string
		ctx     context.Context
		actor   string
		task    entities.Task
		refused error // nil, apierr.ErrForbidden, or servicecontracts.ErrNobodyNamed
	}{
		{"the holder", signedInAs("alice", entities.RoleUser), "alice", heldBy("alice"), nil},
		{"the holder with nobody signed in", context.Background(), "alice", heldBy("alice"), nil},
		{"an administrator, of somebody else's task", signedInAs("boss", "admin"), "boss", heldBy("alice"), nil},
		{"a member, of somebody else's task", signedInAs("mallory", entities.RoleUser), "mallory", heldBy("alice"), apierr.ErrForbidden},
		{"an operator, of a task somebody holds", signedInAs("olga", entities.RoleOperator), "olga", heldBy("alice"), apierr.ErrForbidden},
		{"an operator, of a task offered to a team", signedInAs("olga", entities.RoleOperator), "olga", offered, apierr.ErrForbidden},
		{"an operator, of a task nobody was named for", signedInAs("olga", entities.RoleOperator), "olga", unnamed, nil},
		{"a member, of a task nobody was named for", signedInAs("mallory", entities.RoleUser), "mallory", unnamed, servicecontracts.ErrNobodyNamed},
		{"an administrator's roles, lent to another name", signedInAs("boss", entities.RoleAdmin), "mallory", heldBy("alice"), apierr.ErrForbidden},
		{"nobody at all, of a task nobody holds", context.Background(), "", offered, apierr.ErrForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := handOverCallerFor(c.ctx, c.actor, c.task).mayHandOver(c.task)
			if c.refused == nil && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if c.refused != nil && !errors.Is(err, c.refused) {
				t.Fatalf("got %v, want %v", err, c.refused)
			}
		})
	}
}

// A role held in one organization counts in a request for that organization
// and in no other, as it does at every endpoint gate.
func TestAnAdministratorOfAnotherOrganizationIsNotOneHere(t *testing.T) {
	t.Parallel()
	here, elsewhere := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	account := entities.User{Username: "boss", RolesByOrganization: map[uuid.UUID][]string{elsewhere: {entities.RoleAdmin}}}
	ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, account)
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: here.String()})

	if handOverCallerFor(ctx, "boss", heldBy("alice")).administrator {
		t.Fatal("an administrator of another organization was treated as one of this organization")
	}
}

// Whether the actor holds the task was read from the actor's name before the
// signed-in account was looked at, so a request from one account naming the
// holder was treated as the holder's. An account acting in another's name is
// refused as a stranger is; a call with nobody signed in — the server's own —
// still acts for the name it gives.
func TestAnAccountActingInAnothersNameIsNotThatPerson(t *testing.T) {
	t.Parallel()
	delegated := entities.Task{
		Status: entities.TaskDelegated, DelegationState: entities.DelegationPending,
		Assignee: &entities.User{Username: "alice"}, Owner: &entities.User{Username: "budi"},
	}
	impostors := map[string]context.Context{
		"a member":         signedInAs("mallory", entities.RoleUser),
		"an administrator": signedInAs("boss", entities.RoleAdmin),
		"an operator":      signedInAs("olga", entities.RoleOperator),
		"an account by pointer": context.WithValue(context.Background(), pkgauth.UserContextKey,
			&entities.User{Username: "mallory", Roles: []string{entities.RoleUser}}),
	}
	stranger := handOverCallerFor(signedInAs("mallory", entities.RoleUser), "mallory", heldBy("alice"))
	for who, ctx := range impostors {
		t.Run(who+" naming the holder", func(t *testing.T) {
			t.Parallel()
			caller := handOverCallerFor(ctx, "alice", heldBy("alice"))
			if caller.holdsTask || caller.administrator || caller.takesUnnamed {
				t.Fatalf("the caller was given the holder's standing or their own roles: %+v", caller)
			}
			for action, refusals := range map[string][2]error{
				"hand over": {caller.mayHandOver(heldBy("alice")), stranger.mayHandOver(heldBy("alice"))},
				"release":   {caller.mayRelease(), stranger.mayRelease()},
				"edit":      {caller.mayEdit(), stranger.mayEdit()},
				"hand back": {handOverCallerFor(ctx, "alice", delegated).mayResolve(), stranger.mayResolve()},
			} {
				got, want := refusals[0], refusals[1]
				if !errors.Is(got, apierr.ErrForbidden) || want == nil || got.Error() != want.Error() {
					t.Errorf("%s: got %v, want the refusal a stranger gets: %v", action, got, want)
				}
			}
		})
	}

	t.Run("nobody signed in, naming the holder", func(t *testing.T) {
		t.Parallel()
		caller := handOverCallerFor(context.Background(), "alice", heldBy("alice"))
		if !caller.holdsTask || caller.administrator || caller.takesUnnamed {
			t.Fatalf("the server acting for the holder: %+v; want the holder's standing and no roles", caller)
		}
		for action, err := range map[string]error{
			"hand over": caller.mayHandOver(heldBy("alice")),
			"release":   caller.mayRelease(),
			"edit":      caller.mayEdit(),
			"hand back": handOverCallerFor(context.Background(), "alice", delegated).mayResolve(),
		} {
			if err != nil {
				t.Errorf("%s was refused: %v", action, err)
			}
		}
		if _, err := caller.reasonFor("", "assigning this task"); err != nil {
			t.Errorf("the holder was asked for a reason: %v", err)
		}
	})

	t.Run("an account naming itself", func(t *testing.T) {
		t.Parallel()
		if !handOverCallerFor(signedInAs("alice", entities.RoleUser), "alice", heldBy("alice")).holdsTask {
			t.Fatal("the holder, signed in as themselves, was not treated as the holder")
		}
	})
}

func TestWhoMustSayWhy(t *testing.T) {
	t.Parallel()
	holder := handOverCaller{username: "alice", holdsTask: true}
	administrator := handOverCaller{username: "boss", administrator: true}

	if reason, err := holder.reasonFor("", "assigning this task"); err != nil || reason != "" {
		t.Fatalf("the holder was asked for a reason: %q, %v", reason, err)
	}
	if reason, err := holder.reasonFor("  on leave  ", "assigning this task"); err != nil || reason != "on leave" {
		t.Fatalf("the holder's reason came back as %q, %v; want it trimmed", reason, err)
	}
	for _, empty := range []string{"", "   ", "\n\t"} {
		_, err := administrator.reasonFor(empty, "assigning this task")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "say why you are assigning this task") {
			t.Fatalf("an administrator giving %q as the reason: got %v, want a 400 asking why", empty, err)
		}
	}
	long := strings.Repeat("x", servicecontracts.MaxHandOverReasonLength+1)
	if _, err := holder.reasonFor(long, "assigning this task"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a reason of %d characters: got %v, want a 400", len(long), err)
	}
	if _, err := administrator.reasonFor(strings.Repeat("é", servicecontracts.MaxHandOverReasonLength), "assigning this task"); err != nil {
		t.Fatalf("a reason of exactly the longest length, in two-byte characters, was refused: %v", err)
	}
}
