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
