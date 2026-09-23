// Package instancemigration covers moving work already in flight onto another
// version of a process.
//
// The supported way to change version is to promote a new one and let the old
// drain. This is the case drain cannot serve — a version that must not
// continue — and it rewrites instances that are somebody's purchase order, so
// what it will do has to be answerable before it does it.
package instancemigration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

type fixture struct {
	svc     services.ServiceFacade
	ctx     context.Context
	project uuid.UUID
	// dispatcher is kept so a test can watch what the engine raises. Work being
	// taken out of somebody's hands is an event before it is a notification,
	// and that is the level a migration test can assert at.
	dispatcher observercontracts.EventDispatcher
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(),
		"migration-test", nil, nil, nil, func(*gorm.DB) {})

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tenantCtx, org.ID, "P", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &fixture{svc: svc, ctx: tenantCtx, project: project.ID, dispatcher: dispatcher}
}

// approval is start → one user task → end. The task's id is the parameter,
// because renaming the node work is parked on is the whole reason a mapping
// exists.
func approval(projectID uuid.UUID, taskID string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "expense-approval",
		Name:    "Expense approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: taskID, Name: "Approve", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: taskID},
			{ID: "f2", SourceRef: taskID, TargetRef: "end"},
		},
	}
}

func (f *fixture) deploy(t *testing.T, taskID string) uuid.UUID {
	t.Helper()
	id, err := f.svc.CreateDefinition(f.ctx, approval(f.project, taskID))
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	return id
}

// TestThePlanSaysWhereTheWorkWouldLand is the preview an administrator sees
// before authorising anything.
func TestThePlanSaysWhereTheWorkWouldLand(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")

	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", map[string]any{"amount": 40}); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"approve": "review"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Instances != 1 {
		t.Fatalf("the plan says %d instances would move; one is running", plan.Instances)
	}
	if !plan.Applicable() {
		t.Fatalf("a correct mapping was refused: %v", plan.Refusals)
	}

	var found bool
	for _, move := range plan.Moves {
		if move.From == "approve" && move.To == "review" {
			found = true
			if move.Tasks != 1 {
				t.Errorf("the plan shows %d tasks on approve; one person has it in their inbox", move.Tasks)
			}
			if !move.Mapped {
				t.Error("the plan shows approve→review as carried over; it was mapped")
			}
		}
	}
	if !found {
		t.Fatalf("the plan does not mention the node the work is parked on: %+v", plan.Moves)
	}
}

// TestAPlanThatStrandsWorkIsRefusedAndNothingMoves is the property that makes
// this safe to expose: a refusal costs a retry, and a stranded token cannot be
// un-stranded.
func TestAPlanThatStrandsWorkIsRefusedAndNothingMoves(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	// No mapping: version 2 has no node called "approve", so the task in
	// somebody's inbox would have nowhere to go.
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a migration that would strand work in somebody's inbox was reported as applicable")
	}

	// And the apply refuses for the same reason, rather than half-doing it.
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil); err == nil {
		t.Fatal("the apply accepted what the plan refused")
	}

	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	for _, instance := range instances {
		if instance.Definition != nil && instance.Definition.ID == v2 {
			t.Fatal("a refused migration moved an instance anyway")
		}
	}
}

// TestApplyingMovesTheWorkAndTheInboxItem is the whole point: the person who
// had the task still has it, on the node the new version calls it.
func TestApplyingMovesTheWorkAndTheInboxItem(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"approve": "review"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	tasks, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("there are %d tasks after the migration; the one in somebody's inbox must survive it", len(tasks))
	}
	if tasks[0].NodeID() != "review" {
		t.Fatalf("the task is still on %q; the new version calls that node \"review\"", tasks[0].NodeID())
	}
}
