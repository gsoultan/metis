package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/payloads"
	"github.com/gsoultan/metis/tests/testutils"
)

// An instance's variables are one column, rewritten whole on every step and
// copied whole into a snapshot by every step, so a document an instance
// carried was written again, and kept again, every time the instance moved.
// With a payload directory, the document is kept once, the row and each
// snapshot refer to it, and the engine reads it back as it was.
func TestALargeVariableIsKeptOnceOutsideTheInstanceRow(t *testing.T) {
	db := testutils.SetupTestDB(t)
	dir := t.TempDir()
	if err := payloads.Configure(dir, 16<<10); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(payloads.ResetForTest)

	repo := repositories.NewRepository(db)
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"payload-store-secret", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "payload-review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "review", Type: entities.UserTask, Assignee: "alice", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	document := strings.Repeat("payload-store-marker ", 20_000) // about 420 KB
	instanceID, err := svc.StartProcess(ctx, projectID, "payload-review", map[string]any{
		"document": document,
		"amount":   250.0,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// The engine reads the document back as it was.
	instance, err := repo.Process().Get(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Variables["document"] != document || instance.Variables["amount"] != 250.0 {
		t.Fatalf("the instance read back %d bytes of document and amount %v", len(stringOr(instance.Variables["document"])), instance.Variables["amount"])
	}

	// Rewritten without the document changing, as every step rewrites it.
	for range 3 {
		if err := repo.Process().Update(ctx, instance); err != nil {
			t.Fatalf("rewrite the instance: %v", err)
		}
	}
	files, err := os.ReadDir(filepath.Join(dir, "main", instanceID.String()))
	if err != nil || len(files) != 1 {
		t.Fatalf("the instance's directory holds %d files (%v), want the document once", len(files), err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	sealed, err := root.ReadFile(filepath.Join("main", instanceID.String(), files[0].Name()))
	if err != nil || strings.Contains(string(sealed), "payload-store-marker") {
		t.Errorf("the kept document is in the clear, or unreadable (%v)", err)
	}

	// The row and every snapshot are small.
	var rowBytes int64
	if err := db.WithContext(context.Background()).
		Raw("SELECT octet_length(variables) FROM process_instances WHERE id = ?", instanceID).
		Row().Scan(&rowBytes); err != nil {
		t.Fatalf("measure the row: %v", err)
	}
	if rowBytes > 4<<10 {
		t.Errorf("the instance's variables column is %d bytes, want the reference only", rowBytes)
	}
	var snapshots, largest int64
	if err := db.WithContext(context.Background()).
		Raw("SELECT count(*), coalesce(max(octet_length(variables::text)), 0) FROM variable_snapshots WHERE instance_id = ?", instanceID).
		Row().Scan(&snapshots, &largest); err != nil {
		t.Fatalf("measure the snapshots: %v", err)
	}
	if snapshots == 0 || largest > 4<<10 {
		t.Errorf("%d snapshots, the largest %d bytes: want each to refer to the document", snapshots, largest)
	}
	history, err := repo.VariableSnapshot().ListByInstance(ctx, instanceID)
	if err != nil || len(history) == 0 || history[len(history)-1].Variables["document"] != document {
		t.Errorf("the variable history did not read the document back (%v)", err)
	}

	// The instance moves on with it: the task carries it, and completing the
	// task finishes the instance with it.
	tasks, err := repo.Task().ListByInstance(ctx, instanceID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("read the review task: %d (%v)", len(tasks), err)
	}
	if err := svc.CompleteTask(testutils.AsOperator(ctx, "alice"), uuid.UUID(tasks[0].ID), "alice", nil); err != nil {
		t.Fatalf("complete the review: %v", err)
	}
	finished, err := repo.Process().Get(ctx, instanceID)
	if err != nil || finished.Status != models.ProcessCompleted || finished.Variables["document"] != document {
		t.Errorf("the instance is %s with %d bytes of document (%v), want completed with all of it",
			finished.Status, len(stringOr(finished.Variables["document"])), err)
	}

	// The sweep leaves a live instance's directory, and removes one whose
	// instance is gone.
	gone := filepath.Join(dir, "main", uuid.New().String())
	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	system := entities.WithSystemContext(context.Background())
	removed, err := payloads.SweepGone(system, time.Now().Add(time.Hour), repo.Process().Existing)
	if err != nil || removed != 1 {
		t.Fatalf("the sweep removed %d (%v), want the gone instance's directory", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "main", instanceID.String())); err != nil {
		t.Errorf("the sweep removed a live instance's directory: %v", err)
	}
}

func stringOr(value any) string {
	s, _ := value.(string)
	return s
}
