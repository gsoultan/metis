package task_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
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

	// Columns named explicitly rather than through the model, because the
	// model now carries the default that makes this safe — and the point is to
	// write the row the way something that does not know about the model would.
	id := uuid.New()
	if err := h.db.Exec(`
		INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id,
		                   node_id, name, type, status, assignee, variables)
		VALUES (?, now(), now(), ?, ?, 'approve', 'Approve', 'userTask', 'unclaimed', 'alice', '')`,
		id, h.projID, uuid.New()).Error; err != nil {
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

	err := h.db.Exec(`
		INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id,
		                   node_id, name, type, status, priority, variables)
		VALUES (?, now(), now(), ?, ?, 'approve', 'Approve', 'userTask', 'unclaimed', NULL, '')`,
		uuid.New(), h.projID, uuid.New()).Error
	if err == nil {
		t.Fatal("the database accepted a NULL priority; the scanner cannot read one back")
	}
}
