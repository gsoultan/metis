package impl

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// TestNamedNode pins the repair for the timeline reading `claimed task
// "unknown"`: a stored task references its node by ID alone, and the narrative
// writers need the name the task itself carries.
func TestNamedNode(t *testing.T) {
	tests := []struct {
		name string
		task entities.Task
		want string
	}{
		{
			name: "node without a name borrows the task's",
			task: entities.Task{Name: "Approve the refund", Node: &entities.Node{ID: "approve"}},
			want: "Approve the refund",
		},
		{
			name: "a named node keeps its own name",
			task: entities.Task{Name: "task label", Node: &entities.Node{ID: "n", Name: "Designer name"}},
			want: "Designer name",
		},
		{
			name: "no node at all still names the event",
			task: entities.Task{Name: "Approve the refund"},
			want: "Approve the refund",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node := namedNode(tc.task)
			if node == nil || node.Name != tc.want {
				t.Fatalf("namedNode = %+v, want name %q", node, tc.want)
			}
		})
	}

	t.Run("nothing to name stays nil", func(t *testing.T) {
		if node := namedNode(entities.Task{}); node != nil {
			t.Fatalf("namedNode of an empty task = %+v, want nil", node)
		}
	})

	t.Run("the original node is not mutated", func(t *testing.T) {
		original := &entities.Node{ID: "approve"}
		_ = namedNode(entities.Task{Name: "X", Node: original})
		if original.Name != "" {
			t.Fatal("namedNode wrote the borrowed name back into the shared node")
		}
	})
}

// TestClaimNarrativeNamesTheTask runs the real service over a real database
// and reads the narrative back — the end-to-end form of the repair above,
// which is how the defect was found: the quickstart's timeline said `admin
// claimed task "unknown"` about a task named "Approve the refund".
func TestClaimNarrativeNamesTheTask(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	engine := NewExecutionEngine(repo, observersimpl.NewEventDispatcher())
	svc := NewTaskService(repo, engine, NewAuditWriter(repo.Audit()))

	instance := entities.ProcessInstance{
		ID:         uuid.New(),
		Project:    &entities.Project{ID: uuid.New()},
		Definition: &entities.ProcessDefinition{ID: uuid.New()},
	}
	node := entities.Node{ID: "approve", Name: "Approve the refund", Type: entities.UserTask}
	if err := svc.CreateTaskForNode(t.Context(), instance, node); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tasks, err := svc.ListTasks(t.Context(), uuid.Nil)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("list: %d tasks, err=%v", len(tasks), err)
	}
	if err := svc.ClaimTask(t.Context(), tasks[0].ID, "admin"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	entries, err := repo.Audit().ListByInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	var claimed string
	for _, entry := range entries {
		if entry.Type == EventTaskClaimed {
			claimed = entry.Narrative
		}
	}
	want := `admin claimed task "Approve the refund"`
	if claimed != want {
		t.Fatalf("narrative = %q, want %q — the timeline is the face of the product", claimed, want)
	}
}
