// Package machine_test runs the Servers page's list against a real database:
// each process says it is running, an administrator sees them all, and only a
// machine that stopped can be removed.
package machine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func as(username string, roles ...string) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.User{ID: uuid.New(), Username: username, Roles: roles})
}

func TestEveryProcessIsListedAndOnlyAQuietOneIsForgotten(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	admin := as("ana", entities.RoleAdmin)
	now := time.Now().UTC()

	web := entities.Machine{ID: uuid.New(), Name: "web-1", Address: ":8080", Role: "web", Version: "v1",
		StartedAt: now.Add(-time.Hour), LastSeenAt: now}
	gone := entities.Machine{ID: uuid.New(), Name: "worker-2", Address: ":8080", Role: "worker", Version: "v1",
		StartedAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-10 * time.Minute)}
	for _, m := range []entities.Machine{web, gone} {
		if err := repo.Heartbeat(t.Context(), m); err != nil {
			t.Fatalf("heartbeat %s: %v", m.Name, err)
		}
	}
	// A second heartbeat writes the same row, not another.
	web.LastSeenAt = now.Add(time.Second)
	if err := repo.Heartbeat(t.Context(), web); err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}

	got, err := svc.ListMachines(admin)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].Name != "web-1" || got[1].Name != "worker-2" {
		t.Fatalf("want web-1 then worker-2, the most recently heard first; got %+v", got)
	}
	if got[0].Role != "web" || got[0].Version != "v1" || !got[0].LastSeenAt.Equal(web.LastSeenAt.Truncate(time.Microsecond)) {
		t.Fatalf("web-1 was not kept as it said: %+v", got[0])
	}

	if err := svc.ForgetMachine(admin, web.ID); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a running machine was not refused: %v", err)
	}
	if err := svc.ForgetMachine(admin, gone.ID); err != nil {
		t.Fatalf("forget the quiet one: %v", err)
	}
	if got, _ := svc.ListMachines(admin); len(got) != 1 || got[0].ID != web.ID {
		t.Fatalf("after forgetting worker-2, want only web-1; got %+v", got)
	}
}

func TestOnlyAnAdministratorOfEveryOrganizationSeesTheMachines(t *testing.T) {
	svc := serviceimpl.NewMachineService(repositories.NewRepository(testutils.SetupTestDB(t)).Machine(), nil)
	if _, err := svc.ListMachines(as("bo", entities.RoleOperator)); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator was not refused: %v", err)
	}
	if err := svc.ForgetMachine(as("bo", entities.RoleOperator), uuid.New()); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator was not refused forgetting: %v", err)
	}
}

func TestAProcessSaysItIsRunningUntilItStops(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	self := entities.Machine{ID: uuid.New(), Name: "this-host", Role: "all"}
	svc.StartMachineHeartbeat(ctx, self, nil)

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := svc.ListMachines(as("ana", entities.RoleAdmin))
		if err == nil && len(got) == 1 && got[0].ID == self.ID && !got[0].StartedAt.IsZero() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the process never said it was running: %+v, %v", got, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
