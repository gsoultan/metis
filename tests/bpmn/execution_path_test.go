package bpmn_test

import (
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The execution path is the steps an instance went through, in the order it
// went through them. It was built by walking the audit trail backwards, on the
// assumption that the trail comes newest first. The trail comes oldest first,
// so every path was reported end to start.
func TestTheExecutionPathRunsFromStartToWhereTheInstanceIs(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	// The path is read from the trail, so the trail is written as the server
	// writes it.
	dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(),
		"execution-path-test", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "two-step",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "draft", Type: entities.UserTask, Name: "Draft"},
			{ID: "sign", Type: entities.UserTask, Name: "Sign"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "draft"},
			{ID: "f2", SourceRef: "draft", TargetRef: "sign"},
			{ID: "f3", SourceRef: "sign", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "two-step", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected the draft task: %d tasks, err=%v", len(tasks), err)
	}
	if err := svc.CompleteTask(ctx, tasks[0].ID, "ada", nil); err != nil {
		t.Fatalf("complete the draft: %v", err)
	}

	path, err := svc.GetExecutionPath(ctx, instanceID)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	var got []string
	for _, node := range path.Nodes {
		got = append(got, node.ID)
	}
	if want := []string{"start", "draft", "sign"}; !slices.Equal(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
}
