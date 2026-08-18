package impl

import (
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// An approval matrix — "under 10k the team lead, over 10k the CFO" — is
// business policy, and it changes far more often than the shape of a process.
// Written into a diagram, changing a threshold takes a modeller and a redeploy.
func TestADecisionCanDecideWhoDoesTheWork(t *testing.T) {
	node := entities.Node{ID: "approve", Assignee: "default.approver", Priority: 1}

	resolved := applyAssignment(node, map[string]any{
		"assignee":         "cfo",
		"candidate_groups": "finance, executive",
		"priority":         float64(9),
	})

	if resolved.Assignee != "cfo" {
		t.Errorf("assignee = %q, want cfo", resolved.Assignee)
	}
	if len(resolved.CandidateGroups) != 2 ||
		resolved.CandidateGroups[0].Name != "finance" ||
		resolved.CandidateGroups[1].Name != "executive" {
		t.Errorf("candidate groups = %+v, want finance and executive", resolved.CandidateGroups)
	}
	if resolved.Priority != 9 {
		t.Errorf("priority = %d, want 9", resolved.Priority)
	}
}

// Only what the table actually decided is applied. This is what makes the
// feature additive: a node whose table says nothing behaves as its diagram says.
func TestWhatTheTableDidNotDecideIsLeftAlone(t *testing.T) {
	node := entities.Node{
		ID:       "approve",
		Assignee: "default.approver",
		Priority: 5,
		DueDate:  "2026-01-01T00:00:00Z",
	}

	// A table that decides only the priority.
	resolved := applyAssignment(node, map[string]any{"priority": float64(9)})

	if resolved.Assignee != "default.approver" {
		t.Errorf("assignee = %q; a table that said nothing about it overwrote the diagram", resolved.Assignee)
	}
	if resolved.DueDate != "2026-01-01T00:00:00Z" {
		t.Errorf("due date = %q; a table that said nothing about it overwrote the diagram", resolved.DueDate)
	}
	if resolved.Priority != 9 {
		t.Errorf("priority = %d, want the decided 9", resolved.Priority)
	}

	// And a table that decides nothing at all changes nothing at all.
	untouched := applyAssignment(node, map[string]any{})
	if untouched.Assignee != node.Assignee || untouched.Priority != node.Priority || untouched.DueDate != node.DueDate {
		t.Errorf("an empty decision changed the node: %+v", untouched)
	}
}

// An empty assignee is a table with nothing to say, not an instruction to
// unassign. Treating it as the latter silently takes a task away from whoever
// the modeller sent it to.
func TestAnEmptyOutputDoesNotUnassign(t *testing.T) {
	node := entities.Node{ID: "approve", Assignee: "default.approver"}

	for _, empty := range []any{"", "   ", nil} {
		resolved := applyAssignment(node, map[string]any{"assignee": empty})
		if resolved.Assignee != "default.approver" {
			t.Errorf("an empty assignee (%#v) unassigned the task", empty)
		}
	}
}

// Authors type lists into a grid cell, one column, comma separated. A table
// built by hand can also return a real list.
func TestCandidatesCanBeAListOrOneCell(t *testing.T) {
	node := entities.Node{ID: "approve"}

	fromCell := applyAssignment(node, map[string]any{"candidate_users": "ann, bob ,  carol "})
	if len(fromCell.CandidateUsers) != 3 || fromCell.CandidateUsers[2].Username != "carol" {
		t.Errorf("from a comma-separated cell = %+v, want three trimmed names", fromCell.CandidateUsers)
	}

	fromList := applyAssignment(node, map[string]any{"candidate_users": []any{"ann", "bob"}})
	if len(fromList.CandidateUsers) != 2 || fromList.CandidateUsers[0].Username != "ann" {
		t.Errorf("from a list = %+v, want two names", fromList.CandidateUsers)
	}
}

// A table cannot write an absolute due date — it does not know when the task
// will be created. A duration is how it says "four hours from now".
func TestADueDateCanBeADurationOrAnInstant(t *testing.T) {
	node := entities.Node{ID: "approve"}

	fromInstant := applyAssignment(node, map[string]any{"due_date": "2030-06-01T12:00:00Z"})
	if fromInstant.DueDate != "2030-06-01T12:00:00Z" {
		t.Errorf("due date = %q, want the instant passed through", fromInstant.DueDate)
	}

	before := time.Now()
	fromDuration := applyAssignment(node, map[string]any{"due_date": "PT4H"})
	due, err := time.Parse(time.RFC3339, fromDuration.DueDate)
	if err != nil {
		t.Fatalf("a duration produced %q, which is not a due date: %v", fromDuration.DueDate, err)
	}
	if elapsed := due.Sub(before); elapsed < 3*time.Hour+50*time.Minute || elapsed > 4*time.Hour+10*time.Minute {
		t.Errorf("PT4H resolved to %v from now, want about four hours", elapsed)
	}

	// Something that is neither is left alone rather than guessed at.
	nonsense := applyAssignment(entities.Node{ID: "approve", DueDate: "2026-01-01T00:00:00Z"},
		map[string]any{"due_date": "next tuesday"})
	if nonsense.DueDate != "2026-01-01T00:00:00Z" {
		t.Errorf("an unparseable due date replaced a good one with %q", nonsense.DueDate)
	}
}

func TestANodeSaysWhichTableDecides(t *testing.T) {
	if key, _ := assignmentDecisionOf(entities.Node{ID: "approve"}); key != "" {
		t.Errorf("a node naming no table returned %q", key)
	}

	key, version := assignmentDecisionOf(entities.Node{
		ID: "approve",
		Properties: map[string]any{
			AssignmentDecisionKey:     "approval-matrix",
			AssignmentDecisionVersion: float64(3),
		},
	})
	if key != "approval-matrix" || version != 3 {
		t.Errorf("got %q v%d, want approval-matrix v3", key, version)
	}
}

func TestTheTimelineSaysWhoAndWhy(t *testing.T) {
	message := describeAssignment("approval-matrix", map[string]any{
		"assignee": "cfo",
		"priority": float64(9),
	})
	if message != "approval-matrix set assignee cfo, priority 9" {
		t.Errorf("message = %q", message)
	}

	if got := describeAssignment("approval-matrix", map[string]any{}); got == "" {
		t.Error("a table that decided nothing produced no message at all")
	}
}
