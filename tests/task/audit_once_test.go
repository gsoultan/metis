package task_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Each thing done to a task went into the audit trail twice. The task service
// wrote an entry naming who did it, and then the audit observer wrote its own
// from the event the service raised: a claim became "alice claimed task
// "Review"" and "alice has started working on 'Review'", a release became a
// narrative and "Event: TaskUpdated". The timeline showed every step twice,
// and a mining export counted every activity twice.
func TestEachTaskActionIsAuditedOnce(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	// As the server wires it.
	dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(),
		"audit-once-test", nil, nil, nil, func(*gorm.DB) {})
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "expense-review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review", CandidateUsers: []*entities.User{{Username: "alice"}, {Username: "bob"}}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "expense-review", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected the review task: %d tasks, err=%v", len(tasks), err)
	}
	taskID := tasks[0].ID

	for _, step := range []struct {
		name string
		do   func() error
	}{
		{"claim", func() error { return svc.ClaimTask(ctx, taskID, "alice") }},
		{"release", func() error { return svc.UnclaimTask(ctx, taskID) }},
		{"assign", func() error { return svc.AssignTask(ctx, taskID, "bob") }},
		{"delegate", func() error { return svc.DelegateTask(ctx, taskID, "alice") }},
		{"complete", func() error { return svc.CompleteTask(ctx, taskID, "alice", nil) }},
	} {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}

	entries, err := repo.Audit().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var about []string
	for _, e := range entries {
		// The step being reached is the process's own fact, not a task action.
		if e.NodeID == "review" && e.Type != entities.EventNodeReached {
			about = append(about, e.Type+": "+e.Narrative)
		}
	}
	// Created, claimed, released, assigned, delegated, completed.
	if len(about) != 6 {
		t.Fatalf("six things happened to the task and the trail has %d entries about it:\n  %s",
			len(about), strings.Join(about, "\n  "))
	}
}
