package task_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	taskstore "github.com/gsoultan/metis/server/repositories/store/task"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// TestATaskWrittenWithoutAPriorityIsStillReadable is the regression for the
// panic that took the whole inbox down.
//
// Root cause in one sentence: `tasks.priority` was created nullable because
// AutoMigrate does not carry the model's non-nullability over, while the
// generated scanner decodes it as a fixed-width integer — so a NULL was eight
// bytes read out of an empty buffer, and `GET /api/v1/tasks` panicked with
// `index out of range [7] with length 0`.
//
// It did not return a 500. net/http recovers per connection and closes it, so
// the caller got EOF and the metrics middleware — which records after
// ServeHTTP returns — never counted the request at all. The load test found it
// at a hundred thousand tasks; an installation would have found it the first
// time somebody imported tasks or upgraded onto a release that added the
// column to a table that already had rows.
//
// The insert below is deliberately the shape that caused it: a row written
// without naming priority, which is what a bulk import, a data migration or
// AutoMigrate's own column addition produces. Before the fix it stored NULL
// and this test could not get a response at all.
func TestATaskWrittenWithoutAPriorityIsStillReadable(t *testing.T) {
	h := newTaskHarness(t)

	// Through the store, which writes only the columns it is given: priority
	// is left out, the way a bulk import, a data migration or a column added to
	// a table that already had rows leaves it.
	ex := testutils.Executor(h.db)
	definition := testutils.SeedDefinition(t, h.db, h.projID, "approve")
	instance := testutils.SeedInstance(t, h.db, h.projID, definition, models.ProcessActive)
	id := uuid.Must(uuid.NewV7())
	ins := taskstore.Create()
	ins.SetID(id)
	ins.SetProjectID(h.projID)
	ins.SetInstanceID(instance)
	ins.SetNodeID("approve")
	ins.SetName("Approve")
	ins.SetType("userTask")
	ins.SetStatus("unclaimed")
	ins.SetAssignee("alice")
	ins.SetCandidateUsers(runtime.JSON("[]"))
	ins.SetCandidateGroups(runtime.JSON("[]"))
	ins.SetVariables("")
	if _, err := ins.Insert(t.Context(), ex); err != nil {
		t.Fatalf("write a task without a priority: %v", err)
	}

	status, body := h.get(t, h.tokens["alice"], "/api/v1/tasks?page=1&page_size=25")
	if status != http.StatusOK {
		t.Fatalf("the task list answered %d, not 200: %s", status, body)
	}
	if !strings.Contains(body, id.String()) {
		t.Fatalf("the task written without a priority is missing from the list: %s", body)
	}

	// Zero, not absent: the inbox computes urgency from priority and due date,
	// and a priority that decoded as something else would order the list wrong
	// rather than fail — the kind of wrong nobody reports.
	var page struct {
		Items []struct {
			ID       string `json:"id"`
			Priority int    `json:"priority"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode the task list: %v", err)
	}
	for _, item := range page.Items {
		if item.ID == id.String() && item.Priority != 0 {
			t.Errorf("priority is %d, want 0", item.Priority)
		}
	}
}

// The schema is the thing that actually prevents it. A default alone would let
// an explicit NULL back in; NOT NULL alone would reject the insert above.
func TestThePriorityColumnRefusesNull(t *testing.T) {
	h := newTaskHarness(t)

	priority := testutils.Held(t, h.db).Table("tasks").Column("priority")
	if priority == nil {
		t.Fatal("tasks has no priority column")
	}
	if !priority.NotNull {
		t.Error("tasks.priority accepts NULL; the scanner cannot read one back")
	}
	if priority.Default == "" {
		t.Error("tasks.priority has no default; a row written without one is refused")
	}
}
