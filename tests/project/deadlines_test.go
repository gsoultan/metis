package project_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The dashboard's deadline report read the first page of the task list: the
// newest 200 tasks of any status, from rows that name no process. With three
// late tasks and 205 newer ones, the page held none of the late ones, and none
// of its tasks named a process — every late task would have read "Unknown
// process". The open work with a deadline is read on the server now, soonest
// first, with its process, and all of the open work is counted.
func TestDeadlinesAreReadAcrossAllOfTheOpenWorkWithTheirProcess(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"deadlines-test", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	late := reviewProcess(projectID, "quotation", "Quotation approval")
	late.Nodes[1].DueDate = "2020-01-01T00:00:00Z"
	for _, def := range []*entities.ProcessDefinition{late, reviewProcess(projectID, "onboarding", "Onboarding")} {
		if _, err := svc.CreateDefinition(ctx, def); err != nil {
			t.Fatalf("deploy %s: %v", def.Key, err)
		}
	}
	for range 4 {
		if _, err := svc.StartProcess(ctx, projectID, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
	}
	for range 205 {
		if _, err := svc.StartProcess(ctx, projectID, "onboarding", nil); err != nil {
			t.Fatalf("start an onboarding: %v", err)
		}
	}
	// One late task is done, so it is no longer anybody's deadline.
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.DueDate != nil {
			if err := svc.CompleteTask(ctx, task.ID, "ada", nil); err != nil {
				t.Fatalf("complete a late task: %v", err)
			}
			break
		}
	}

	// Another organization's late work is not this project's.
	otherCtx, _, otherProject := testutils.ScopedProject(t, repo)
	elsewhere := reviewProcess(otherProject, "quotation", "Quotation approval")
	elsewhere.Nodes[1].DueDate = "2020-01-01T00:00:00Z"
	if _, err := svc.CreateDefinition(otherCtx, elsewhere); err != nil {
		t.Fatalf("deploy elsewhere: %v", err)
	}
	if _, err := svc.StartProcess(otherCtx, otherProject, "quotation", nil); err != nil {
		t.Fatalf("start elsewhere: %v", err)
	}

	deadlines, err := svc.Deadlines(ctx, projectID)
	if err != nil {
		t.Fatalf("deadlines: %v", err)
	}
	if deadlines.WithDeadline != 3 || deadlines.WithoutDeadline != 205 {
		t.Fatalf("counted %d open tasks with a deadline and %d without, want 3 and 205",
			deadlines.WithDeadline, deadlines.WithoutDeadline)
	}
	if len(deadlines.Tasks) != 3 {
		t.Fatalf("read %d open tasks with a deadline, want the 3 that are late", len(deadlines.Tasks))
	}
	due := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, task := range deadlines.Tasks {
		if task.ProcessName != "Quotation approval" || task.ProcessKey != "quotation" {
			t.Errorf("a late task names its process %q (%q)", task.ProcessName, task.ProcessKey)
		}
		if !task.DueDate.Equal(due) || task.NodeID != "review" || task.Name != "Review" {
			t.Errorf("a late task came back as %+v", task)
		}
	}

	// And a caller outside the organization reads none of it.
	if foreign, err := svc.Deadlines(otherCtx, projectID); err == nil && (len(foreign.Tasks) != 0 || foreign.WithoutDeadline != 0) {
		t.Errorf("another organization read this project's deadlines: %+v", foreign)
	}
}
