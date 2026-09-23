package instancemigration

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Writing a row that was read before the lock.
//
// The race test next door is probabilistic: it needs the scheduler to land a
// completion inside a window a few statements wide, and usually it does not.
// This one puts the completion there on purpose, by hooking the listing the
// migration does and running the completion the moment it returns. No
// goroutines, no timing, same defect.

// hookedRepository delegates everything and runs a function at a chosen call of
// ListByDefinition — the read whose result apply used to keep and write back.
type hookedRepository struct {
	repositories.Repository
	process *hookedProcess
}

func (r *hookedRepository) Process() repocontracts.ProcessRepository { return r.process }

type hookedProcess struct {
	repocontracts.ProcessRepository
	// on is the call number to fire at. PlanInstanceMigration lists first and
	// apply lists second, and the defect lives between apply's list and apply's
	// write — so the interesting one is the second.
	on    int
	calls int
	fire  func()
	once  sync.Once
	mu    sync.Mutex
}

func (p *hookedProcess) ListByDefinition(ctx context.Context, id uuid.UUID) ([]models.ProcessInstanceModel, error) {
	out, err := p.ProcessRepository.ListByDefinition(ctx, id)
	p.mu.Lock()
	p.calls++
	reached := p.calls == p.on
	p.mu.Unlock()
	if err == nil && reached && p.fire != nil {
		p.once.Do(p.fire)
	}
	return out, err
}

// TestAnInstanceThatFinishesMidMigrationIsNotResurrected.
//
// Root cause: apply read the instances, then for each one took the row lock,
// discarded what the lock returned, and wrote back the copy it had read before
// the transaction. A completion committing in between was overwritten whole —
// the instance came back with the status and the tokens it had before somebody
// finished it, and their completed approval reappeared as work still to do.
func TestAnInstanceThatFinishesMidMigrationIsNotResurrected(t *testing.T) {
	db := testutils.SetupTestDB(t)
	plain := repositories.NewRepository(testutils.StormConn(db))

	hooked := &hookedRepository{
		Repository: plain,
		process:    &hookedProcess{ProcessRepository: plain.Process(), on: 2},
	}

	// Two facades over one database: the migration runs on the hooked one, the
	// completion on the plain one, so the completion is somebody else's
	// transaction exactly as it would be in production.
	newFacade := func(repo repositories.Repository) services.ServiceFacade {
		return services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
			"stale-write-test", nil, nil, nil, func(*gorm.DB) {})
	}
	migrator := newFacade(hooked)
	worker := newFacade(plain)

	ctx := context.Background()
	org, err := worker.CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := worker.CreateProject(tenantCtx, org.ID, "P", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	v1, err := worker.CreateDefinition(tenantCtx, approval(project.ID, "approve"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := worker.StartProcess(tenantCtx, project.ID, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := worker.CreateDefinition(tenantCtx, approval(project.ID, "review"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// The completion happens the moment apply has listed the instances and
	// before it has written any of them.
	var completeErr error
	hooked.process.fire = func() {
		tasks, err := worker.ListTasks(tenantCtx, project.ID)
		if err != nil {
			completeErr = err
			return
		}
		for _, task := range tasks {
			if task.NodeID() == "approve" && task.Status == entities.TaskClaimed {
				completeErr = worker.CompleteTask(tenantCtx, task.ID, "ada", nil)
				return
			}
		}
	}

	if err := migrator.MigrateInstances(tenantCtx, v1, v2, map[string]string{"approve": "review"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if completeErr != nil {
		t.Fatalf("the completion that races the migration failed: %v", completeErr)
	}
	if hooked.process.calls < 2 {
		t.Fatalf("the hook never reached apply's listing (%d calls); the test is not exercising the window", hooked.process.calls)
	}

	instances, err := worker.ListInstances(tenantCtx, project.ID)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected one instance, found %d", len(instances))
	}
	instance := instances[0]

	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("the instance is %q; it was completed before the migration wrote, and a migration must not undo that",
			instance.Status)
	}
	if len(instance.Tokens) != 0 {
		t.Fatalf("a completed instance holds %d token(s); the migration wrote back the tokens it read before the completion",
			len(instance.Tokens))
	}
	all, err := worker.ListTasks(tenantCtx, project.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range all {
		switch task.Status {
		case entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated:
			t.Fatalf("task on %q is open again after being completed; the migration resurrected it", task.NodeID())
		}
	}
}
