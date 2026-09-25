package userimport_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/repositories/pg"
)

// TestARemovedParticipantIsReinstatedRatherThanDuplicated is the behaviour the
// soft-delete upgrade would have broken quietly.
//
// storm v0.8.0+ compiles `deleted_at IS NULL` into every read, so the check
// "have I seen this person before" stops seeing somebody who was removed. Left
// alone, an import naming them again would report them as created and upsert
// onto their still-marked row: the row would keep its tasks and its group
// memberships, stay invisible, and the person would appear to have been added
// and then not be there.
//
// It runs against PostgreSQL because the property is in the DDL — a unique key
// declared across the deleted rows — and nothing about it is observable from a
// unit test.
func TestARemovedParticipantIsReinstatedRatherThanDuplicated(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)

	first, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username,email\nada,ada@example.com\n"))
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if first.Created != 1 {
		t.Fatalf("the first import created %d participants; one was in the file", first.Created)
	}

	directory := pg.NewWorkflowUserRepository(conn)
	ada, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("read the participant back: %v", err)
	}
	originalID := ada.ID

	// Marked directly, because the directory has no delete of its own yet —
	// nothing in the product removes a participant today. The column and the
	// declaration are what make the behaviour safe when it does, and that is
	// what this asserts.
	if _, err := conn.Main().Exec(ctx,
		"UPDATE workflow_users SET deleted_at = now() WHERE id = $1", ada.ID); err != nil {
		t.Fatalf("mark the participant removed: %v", err)
	}
	if _, err := directory.GetByUsername(ctx, projectID, "ada"); err == nil {
		t.Fatal("a removed participant is still returned by a read; the soft-delete predicate is not being applied")
	}

	// Named again by a later import: somebody saying they are back.
	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username,email\nada,ada@example.com\n")); err != nil {
		t.Fatalf("second import: %v", err)
	}

	back, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("the participant was not reinstated by an import that named them: %v", err)
	}
	if back.ID != originalID {
		t.Fatalf("reinstating gave them a new id (%s, was %s); their tasks and group memberships hang off the old one",
			back.ID, originalID)
	}

	// And exactly one row, not a live one beside a marked one.
	var rows int
	if err := conn.Main().QueryRow(ctx,
		"SELECT count(*) FROM workflow_users WHERE project_id = $1 AND username = 'ada'", projectID).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("there are %d rows for ada; the key is declared across the deleted rows so there must be one", rows)
	}
}

// TestARemovedParticipantsUsernameIsNotFreeForSomebodyElse states the other
// half plainly. It is the reason the key is UniqueAcrossDeleted rather than the
// live-only default storm would otherwise emit.
func TestARemovedParticipantsUsernameIsNotFreeForSomebodyElse(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)

	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username,email\nada,ada@example.com\n")); err != nil {
		t.Fatalf("import: %v", err)
	}
	directory := pg.NewWorkflowUserRepository(conn)
	ada, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if _, err := conn.Main().Exec(ctx,
		"UPDATE workflow_users SET deleted_at = now() WHERE id = $1", ada.ID); err != nil {
		t.Fatalf("mark removed: %v", err)
	}

	// A raw insert, because the service's upsert would find the marked row and
	// reinstate it — which is right, and not what is being tested here. This is
	// the database being asked directly whether the name is free.
	_, err = conn.Main().Exec(ctx,
		"INSERT INTO workflow_users (project_id, username, active) VALUES ($1, 'ada', true)", projectID)
	if err == nil {
		t.Fatal("a second row claimed a removed participant's username; their history would be split between two ids")
	}
}

// TestRemovingSomebodyTakesThemOutOfTheDirectory covers the operation the
// product was missing: the column, the index and the key were all in place and
// nothing could remove anybody.
func TestRemovingSomebodyTakesThemOutOfTheDirectory(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)

	if _, err := svc.ImportWorkflowUsers(ctx, projectID,
		strings.NewReader("username,email\nada,ada@example.com\ngrace,grace@example.com\n")); err != nil {
		t.Fatalf("import: %v", err)
	}
	directory := pg.NewWorkflowUserRepository(conn)
	ada, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if err := svc.RemoveWorkflowUser(ctx, projectID, ada.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(people) != 1 || people[0].Username != "grace" {
		t.Fatalf("after removing ada the directory holds %d people (%v); it should hold grace alone", len(people), people)
	}

	// Removing again is not found, not a second success: the caller asked to
	// remove somebody who is not there.
	if err := svc.RemoveWorkflowUser(ctx, projectID, ada.ID); err == nil {
		t.Fatal("removing the same participant twice succeeded the second time")
	}

	// And an import naming them brings back the same person, not a new one.
	if _, err := svc.ImportWorkflowUsers(ctx, projectID,
		strings.NewReader("username,email\nada,ada@example.com\n")); err != nil {
		t.Fatalf("reimport: %v", err)
	}
	back, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("ada was not reinstated: %v", err)
	}
	if back.ID != ada.ID {
		t.Fatalf("reinstating gave a new id %s (was %s)", back.ID, ada.ID)
	}
}

// TestSomebodyElsesParticipantCannotBeRemoved is the check that makes the id in
// the path safe: on its own it would let a caller who manages one project's
// directory remove a person from another's.
func TestSomebodyElsesParticipantCannotBeRemoved(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)

	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username\nada\n")); err != nil {
		t.Fatalf("import: %v", err)
	}
	directory := pg.NewWorkflowUserRepository(conn)
	ada, err := directory.GetByUsername(ctx, projectID, "ada")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var otherProject uuid.UUID
	if err := conn.Main().QueryRow(ctx,
		`INSERT INTO projects (organization_id, name)
		 SELECT organization_id, 'Elsewhere' FROM projects WHERE id = $1 RETURNING id`,
		projectID).Scan(&otherProject); err != nil {
		t.Fatalf("seed a second project: %v", err)
	}

	if err := svc.RemoveWorkflowUser(ctx, otherProject, ada.ID); err == nil {
		t.Fatal("a participant was removed by naming a project they do not belong to")
	}
	if _, err := directory.GetByUsername(ctx, projectID, "ada"); err != nil {
		t.Fatalf("the refused removal took them out anyway: %v", err)
	}
}
