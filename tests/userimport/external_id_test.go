package userimport_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
)

func csvImport(body string) entities.DirectoryImport {
	return entities.DirectoryImport{Kind: "csv", CSV: []byte(body)}
}

func problemFor(summary entities.ImportSummary, line int) string {
	for _, p := range summary.Problems {
		if p.Line == line {
			return p.Reason
		}
	}
	return ""
}

// Somebody imported before external IDs existed is linked on their username,
// not created twice; after that their external ID is what finds them.
func TestAnExternalIDLinksTheExistingPersonAndThenFindsThem(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	if _, err := svc.ImportDirectory(ctx, projectID, csvImport("username,email\nada,ada@example.com\n")); err != nil {
		t.Fatal(err)
	}

	summary, err := svc.ImportDirectory(ctx, projectID, csvImport("external_id,username,email\n1001,ada,ada@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Created != 0 || summary.Updated != 1 {
		t.Fatalf("ada should be linked, not created: %+v", summary)
	}
	person, err := svc.FindWorkflowUserByExternalID(ctx, projectID, "1001")
	if err != nil || person.Username != "ada" {
		t.Fatalf("1001 should be ada, got %+v, %v", person, err)
	}

	// A source without the column leaves the link alone.
	if _, err := svc.ImportDirectory(ctx, projectID, csvImport("username,email\nada,ada@example.com\n")); err != nil {
		t.Fatal(err)
	}
	if person, _ := svc.FindWorkflowUserByExternalID(ctx, projectID, "1001"); person.Username != "ada" {
		t.Fatal("an import without external IDs must not unlink anybody")
	}
}

// A row that would take a username or an external ID from somebody else is
// refused with its reason, and the rows around it still import.
func TestAConflictingRowIsRefusedAndTheRestImports(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	if _, err := svc.ImportDirectory(ctx, projectID, csvImport("external_id,username\n1001,ada\n1002,bob\n")); err != nil {
		t.Fatal(err)
	}

	summary, err := svc.ImportDirectory(ctx, projectID, csvImport(
		"external_id,username\n"+
			"1003,ada\n"+ // ada already belongs to 1001
			"1001,bob\n"+ // 1001 renamed to bob, but bob is 1002
			"1004,carol\n"+
			"1004,dan\n")) // the same id twice
	if err != nil {
		t.Fatal(err)
	}
	if summary.Created != 1 {
		t.Fatalf("only carol is new: %+v", summary)
	}
	for line, want := range map[int]string{2: "already belongs to external ID", 3: "already belongs to somebody else", 5: "already named on line 4"} {
		if reason := problemFor(summary, line); !strings.Contains(reason, want) {
			t.Errorf("line %d: want %q, got %q", line, want, reason)
		}
	}
}

// A check writes nothing, and says per row what an import would do.
func TestACheckWritesNothing(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	if _, err := svc.ImportDirectory(ctx, projectID, csvImport("external_id,username,email\n1001,ada,ada@example.com\n1002,bob,bob@example.com\n")); err != nil {
		t.Fatal(err)
	}

	check, err := svc.CheckDirectoryImport(ctx, projectID, csvImport(
		"external_id,username,email\n"+
			"1001,ada,ada@example.com\n"+
			"1002,bob,robert@example.com\n"+
			"1003,carol,carol@example.com\n"+
			"1004,,dan@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"unchanged": 1, "update": 1, "new": 1, "invalid": 1}
	for outcome, n := range want {
		if check.Counts[outcome] != n {
			t.Errorf("%s: want %d, got %d (%v)", outcome, n, check.Counts[outcome], check.Counts)
		}
	}
	if check.Read != 4 || len(check.Rows) != 3 {
		t.Fatalf("four rows read, three worth listing: %+v", check)
	}
	if check.Rows[0].Line != 3 || !strings.Contains(check.Rows[0].Detail, `email was "bob@example.com"`) {
		t.Fatalf("bob's change should be described, got %+v", check.Rows[0])
	}

	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 || people[1].Email != "bob@example.com" {
		t.Fatalf("a check must write nothing, got %+v", people)
	}
}

// A source keeps its own field names; the mapping says which is which, nested
// JSON fields included. Fields it does not name are dropped.
func TestAMappingReadsTheSourcesOwnNames(t *testing.T) {
	result, err := userimport.FromJSONWith([]byte(`{"data": [
		{"id": 7, "login": "ada", "profile": {"full_name": "Ada Lovelace"}, "status": "inactive", "salary": 100}
	]}`), userimport.Mapping{
		"external_id": "id", "username": "login", "display_name": "profile.full_name", "active": "status",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("ada should be read, got %+v", result)
	}
	row := result.Rows[0]
	if row.ExternalID != "7" || row.Username != "ada" || row.DisplayName != "Ada Lovelace" || row.Active {
		t.Fatalf("read wrongly: %+v", row)
	}
	if !strings.Contains(strings.Join(result.Fields, ","), "profile.full_name") {
		t.Fatalf("the source's own names should be reported for matching, got %v", result.Fields)
	}

	if _, err := userimport.FromJSONWith([]byte(`[]`), userimport.Mapping{"salary": "pay"}); err == nil {
		t.Fatal("a mapping to a field Metis does not import should be refused")
	}

	csv, err := userimport.ParseWith(strings.NewReader("Login,Mail\nada,ada@example.com\n"), userimport.Mapping{"username": "login", "email": "mail"})
	if err != nil || len(csv.Rows) != 1 || csv.Rows[0].Email != "ada@example.com" {
		t.Fatalf("a CSV should read through the mapping too, got %+v, %v", csv, err)
	}
}

// External IDs are unique within a project, not across them: another project
// — or another organization — may have its own 1001, and they are two people.
func TestTheSameExternalIDInTwoProjectsIsTwoPeople(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	other := secondProject(ctx, t)

	for _, project := range []uuid.UUID{projectID, other} {
		summary, err := svc.ImportDirectory(ctx, project, csvImport("external_id,username\n1001,ada\n"))
		if err != nil || summary.Created != 1 {
			t.Fatalf("1001 should be created in each project: %+v, %v", summary, err)
		}
	}
	first, _ := svc.FindWorkflowUserByExternalID(ctx, projectID, "1001")
	second, _ := svc.FindWorkflowUserByExternalID(ctx, other, "1001")
	if first.ID == uuid.Nil || first.ID == second.ID {
		t.Fatalf("two projects' 1001 must be two people: %v and %v", first.ID, second.ID)
	}
}

// secondProject adds a project in another organization to the fixture's
// database.
func secondProject(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	conn := connOf(t)
	var orgID, projectID uuid.UUID
	if err := conn.Main().QueryRow(ctx, "INSERT INTO organizations (name) VALUES ('Globex') RETURNING id").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := conn.Main().QueryRow(ctx,
		"INSERT INTO projects (organization_id, name) VALUES ($1, 'Payments') RETURNING id", orgID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	return projectID
}

// One person is the target of one row. A second row reaching them by another
// route — here, by external ID under a new username after a first row named
// them by their username — is a conflict, not a second write.
func TestOnePersonIsMatchedByOneRow(t *testing.T) {
	existing := []entities.WorkflowUser{{ID: uuid.New(), Username: "ada", ExternalID: "1001", Active: true}}
	plans := userimport.Plan(existing, []userimport.Row{
		{Line: 2, Username: "ada", Active: true},
		{Line: 3, Username: "ada.l", ExternalID: "1001", Active: true},
	})
	if plans[0].Outcome != userimport.OutcomeUnchanged {
		t.Fatalf("line 2 is ada as she is, got %s", plans[0].Outcome)
	}
	if plans[1].Outcome != userimport.OutcomeConflict || !strings.Contains(plans[1].Reason, "line 2") {
		t.Fatalf("line 3 reaches ada again and should be refused, got %s: %s", plans[1].Outcome, plans[1].Reason)
	}
}
