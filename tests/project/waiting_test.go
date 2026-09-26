package project_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// reviewProcess is start → review → end, under key; every one of them names
// its step "review", which is the point: a step id is unique only within one
// process.
func reviewProcess(projectID uuid.UUID, key, name string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    name,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}
}

// The heat map counted the newest 25 instances on the client, and read each
// one's process from a field the server never sends — so every process was
// "Unknown process", steps with the same id were added together across
// processes, and at volume the longest-stuck work was the first to fall off
// the page. It is counted on the server now, across all of it.
func TestWaitingWorkIsCountedPerProcessAcrossAllOfIt(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"waiting-test", nil, nil, nil, func(*gorm.DB) {})
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	start := func(ctx context.Context, project uuid.UUID, key string, n int) {
		t.Helper()
		for range n {
			if _, err := svc.StartProcess(ctx, project, key, nil); err != nil {
				t.Fatalf("start %s: %v", key, err)
			}
		}
	}
	for _, def := range []*entities.ProcessDefinition{
		reviewProcess(projectID, "quotation", "Quotation approval"),
		reviewProcess(projectID, "onboarding", "Onboarding"),
	} {
		if _, err := svc.CreateDefinition(ctx, def); err != nil {
			t.Fatalf("deploy %s: %v", def.Key, err)
		}
	}
	start(ctx, projectID, "quotation", 30) // more than a page
	start(ctx, projectID, "onboarding", 2)

	// One onboarding finishes: it is no longer waiting anywhere.
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Name == "Review" {
			instance, err := svc.GetInstance(ctx, task.Instance.ID)
			if err == nil && instance.Definition != nil {
				def, err := svc.GetDefinition(ctx, instance.Definition.ID)
				if err == nil && def.Key == "onboarding" {
					if err := svc.CompleteTask(ctx, task.ID, "ada", nil); err != nil {
						t.Fatalf("complete: %v", err)
					}
					break
				}
			}
		}
	}

	// Another organization's identical process is not this project's work.
	otherCtx, _, otherProject := testutils.ScopedProject(t, repo)
	if _, err := svc.CreateDefinition(otherCtx, reviewProcess(otherProject, "quotation", "Quotation approval")); err != nil {
		t.Fatalf("deploy elsewhere: %v", err)
	}
	start(otherCtx, otherProject, "quotation", 5)

	waiting, err := svc.WaitingByStep(ctx, projectID)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	got := map[string]entities.WaitingProcess{}
	for _, process := range waiting {
		got[process.Key] = process
	}
	if len(got) != 2 {
		t.Fatalf("waiting work in %d processes, want 2: %+v", len(got), waiting)
	}
	for key, want := range map[string]struct {
		name      string
		instances int
	}{
		"quotation":  {"Quotation approval", 30},
		"onboarding": {"Onboarding", 1},
	} {
		process := got[key]
		if process.Name != want.name || process.Instances != want.instances ||
			len(process.Steps) != 1 || process.Steps[0].NodeID != "review" || process.Steps[0].Waiting != want.instances {
			t.Errorf("%s: %+v, want %d waiting at review, named %q", key, process, want.instances, want.name)
		}
	}

	// And a caller outside the organization sees none of it.
	if foreign, err := svc.WaitingByStep(otherCtx, projectID); err == nil && len(foreign) != 0 {
		t.Errorf("another organization read this project's waiting work: %+v", foreign)
	}
}
