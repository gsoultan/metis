package machine_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// running records machines that said they are running now.
func running(t *testing.T, repo contracts.MachineRepository, machines ...entities.Machine) []entities.Machine {
	t.Helper()
	now := time.Now().UTC()
	for i := range machines {
		if machines[i].ID == uuid.Nil {
			machines[i].ID = uuid.New()
		}
		machines[i].StartedAt, machines[i].LastSeenAt = now.Add(-time.Hour), now
		if err := repo.Heartbeat(t.Context(), machines[i]); err != nil {
			t.Fatalf("heartbeat %s: %v", machines[i].Name, err)
		}
	}
	return machines
}

func asked(t *testing.T, repo contracts.MachineRepository, id uuid.UUID) string {
	t.Helper()
	role, err := repo.RequestedRole(t.Context(), id)
	if err != nil {
		t.Fatalf("read the asked role: %v", err)
	}
	return role
}

func TestTheLastWorkerCannotBeSwitchedToWeb(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	admin := as("ana", entities.RoleAdmin)
	m := running(t, repo,
		entities.Machine{Name: "web-1", Role: "web"},
		entities.Machine{Name: "worker-1", Role: "worker"})
	web, worker := m[0], m[1]

	if err := svc.ChangeMachineRole(admin, worker.ID, "web"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("switching the only worker to web was not refused: %v", err)
	}
	if got := asked(t, repo, worker.ID); got != "" {
		t.Fatalf("a refused change was kept: %q", got)
	}

	// With web-1 asked to work, worker-1 is no longer the last one.
	if err := svc.ChangeMachineRole(admin, web.ID, "worker"); err != nil {
		t.Fatalf("switch web-1 to worker: %v", err)
	}
	if err := svc.ChangeMachineRole(admin, worker.ID, "web"); err != nil {
		t.Fatalf("switch worker-1 to web once web-1 works: %v", err)
	}
	if asked(t, repo, web.ID) != "worker" || asked(t, repo, worker.ID) != "web" {
		t.Fatal("the asks were not kept")
	}

	// Asking web-1 for the role it has takes the ask back, which would
	// leave nobody working: refused, and the ask stays.
	if err := svc.ChangeMachineRole(admin, web.ID, "web"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("taking back the only worker's ask was not refused: %v", err)
	}
	if err := svc.ChangeMachineRole(admin, worker.ID, "worker"); err != nil {
		t.Fatalf("take back worker-1's ask: %v", err)
	}
	if got := asked(t, repo, worker.ID); got != "" {
		t.Fatalf("asking for the role it has did not take the ask back: %q", got)
	}

	// The heartbeat leaves the ask as it is.
	running(t, repo, web)
	if got := asked(t, repo, web.ID); got != "worker" {
		t.Fatalf("a heartbeat cleared the ask: %q", got)
	}
}

func TestAMachineItsOperatorFixedOrThatStoppedIsNotSwitched(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	admin := as("ana", entities.RoleAdmin)
	m := running(t, repo,
		entities.Machine{Name: "fixed", Role: "all", RoleFixed: true},
		entities.Machine{Name: "worker-1", Role: "worker"})

	if err := svc.ChangeMachineRole(admin, m[0].ID, "web"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a machine whose operator set its role was switched: %v", err)
	}
	if err := svc.ChangeMachineRole(admin, uuid.New(), "web"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a machine that is not running was switched: %v", err)
	}
	if err := svc.ChangeMachineRole(admin, m[1].ID, "boss"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a role that does not exist was taken: %v", err)
	}
	if err := svc.ChangeMachineRole(as("bo", entities.RoleOperator), m[1].ID, "all"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator switched a machine: %v", err)
	}
}

// Two administrators each switch a different worker to web at once, with
// nothing else working: only one may win, or both machines stop working.
func TestTwoSwitchesAtOnceCannotBothLeaveNoWorker(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	admin := as("ana", entities.RoleAdmin)
	m := running(t, repo,
		entities.Machine{Name: "web-1", Role: "web"},
		entities.Machine{Name: "worker-1", Role: "worker"},
		entities.Machine{Name: "worker-2", Role: "worker"})

	for round := range 10 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, worker := range m[1:] {
			wg.Go(func() { errs[i] = svc.ChangeMachineRole(admin, worker.ID, "web") })
		}
		wg.Wait()
		won := 0
		for _, err := range errs {
			if err == nil {
				won++
			}
		}
		if won != 1 {
			t.Fatalf("round %d: %d switches won, want exactly 1: %v", round, won, errs)
		}
		for _, worker := range m[1:] {
			if err := repo.RequestRole(t.Context(), worker.ID, "worker", time.Now().Add(-time.Minute)); err != nil {
				t.Fatalf("round %d: put %s back: %v", round, worker.Name, err)
			}
		}
	}
}

func TestAMachineHearsTheRoleItWasAskedToTakeOnce(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	svc := serviceimpl.NewMachineService(repo, nil)
	m := running(t, repo,
		entities.Machine{Name: "this-host", Role: "all"},
		entities.Machine{Name: "worker-1", Role: "worker"})
	if err := svc.ChangeMachineRole(as("ana", entities.RoleAdmin), m[0].ID, "web"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	heard := make(chan string, 2)
	svc.StartMachineHeartbeat(t.Context(), m[0], func(role string) { heard <- role })
	select {
	case role := <-heard:
		if role != "web" {
			t.Fatalf("heard %q, want web", role)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the machine never heard it was asked to change")
	}
}
