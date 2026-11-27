package people_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A person renamed in the customer's application keeps their identity here:
// the import finds them by external ID, renames them, and — when told to —
// readdresses their open tasks, held and offered alike, to the new username.
// Without being told, the rename is refused and reported rather than leaving
// work addressed to a name nobody has.
func TestARenameCarriesItsOpenTasksOnlyWhenAsked(t *testing.T) {
	w := newWorld(t)
	ctx := entities.WithTenantContext(t.Context(), w.tenant(t))
	csv := func(body string) entities.DirectoryImport {
		return entities.DirectoryImport{Kind: "csv", CSV: []byte(body)}
	}

	if _, err := w.svc.ImportDirectory(ctx, w.project, csv("external_id,username,email\nu-1,margaret,m@example.com\n")); err != nil {
		t.Fatalf("first import: %v", err)
	}
	before := w.personID(t, "margaret")

	held := w.startTask(t, w.project, "held-by-margaret", entities.Node{Assignee: "margaret"})
	offered := w.startTask(t, w.project, "offered-to-margaret", entities.Node{
		CandidateUsers: []*entities.User{{Username: "margaret"}, {Username: "grace"}},
	})

	renamed := csv("external_id,username,email\nu-1,m.hamilton,m@example.com\n")

	// The check says what would happen, and changes nothing.
	check, err := w.svc.CheckDirectoryImport(ctx, w.project, renamed)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.Counts["renamed"] != 1 || len(check.Rows) != 1 || check.Rows[0].OpenTasks != 2 {
		t.Fatalf("the check should report one rename with two open tasks, got %+v", check)
	}
	if who, _ := w.holder(t, held.ID); who != "margaret" {
		t.Fatalf("a check must write nothing, but the task is now %q's", who)
	}

	// Not told to move the tasks: refused, reported, nothing changes.
	summary, err := w.svc.ImportDirectory(ctx, w.project, renamed)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Renamed != 0 || len(summary.Problems) != 1 {
		t.Fatalf("the rename should be refused with a reason, got %+v", summary)
	}
	if w.personID(t, "margaret") != before {
		t.Fatal("a refused rename must leave the person as they were")
	}

	// Told to: renamed in place, and both tasks follow.
	renamed.MoveOpenTasks = true
	summary, err = w.svc.ImportDirectory(ctx, w.project, renamed)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Renamed != 1 || summary.TasksMoved != 2 || summary.Created != 0 || len(summary.Problems) != 0 {
		t.Fatalf("expected one rename moving two tasks, got %+v", summary)
	}
	if w.personID(t, "m.hamilton") != before {
		t.Fatal("the renamed person must be the same person, not a new one")
	}
	if who, _ := w.holder(t, held.ID); who != "m.hamilton" {
		t.Fatalf("the held task should follow the rename, it is %q's", who)
	}
	task, err := w.svc.GetTask(ctx, offered.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, user := range task.CandidateUsers {
		names = append(names, user.Username)
	}
	if len(names) != 2 || names[0] != "m.hamilton" || names[1] != "grace" {
		t.Fatalf("the offered task should name m.hamilton in margaret's place, got %v", names)
	}
}
