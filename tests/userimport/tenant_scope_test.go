package userimport_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// TestAnotherOrganizationNeitherReadsNorWritesAProjectsDirectory: a
// project's directory is its people's names and addresses, and a project id
// is no secret. Somebody signed in to another organization was answered with
// all of it, and could import people into it.
func TestAnotherOrganizationNeitherReadsNorWritesAProjectsDirectory(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)
	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username,email,groups\nada,ada@example.com,approvers\n")); err != nil {
		t.Fatalf("import: %v", err)
	}

	var ownOrg uuid.UUID
	if err := conn.Main().QueryRow(ctx, "SELECT organization_id FROM projects WHERE id = $1", projectID).Scan(&ownOrg); err != nil {
		t.Fatal(err)
	}
	var otherOrg uuid.UUID
	if err := conn.Main().QueryRow(ctx, "INSERT INTO organizations (name) VALUES ('Elsewhere') RETURNING id").Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}

	own := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: ownOrg.String()})
	if people, err := svc.ListWorkflowUsers(own, projectID, 0); err != nil || len(people) != 1 {
		t.Fatalf("its own organization lists %v, %v; want ada", people, err)
	}

	other := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: otherOrg.String()})
	if people, err := svc.ListWorkflowUsers(other, projectID, 0); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization lists %v, %v; want not found", people, err)
	}
	summary, err := svc.ImportWorkflowUsers(other, projectID, strings.NewReader("username,email\nmallory,mallory@example.com\n"))
	if err == nil && summary.Created > 0 {
		t.Errorf("another organization imported into the project: %+v", summary)
	}
	var mallory int
	if err := conn.Main().QueryRow(ctx, "SELECT count(*) FROM workflow_users WHERE username = 'mallory'").Scan(&mallory); err != nil {
		t.Fatal(err)
	}
	if mallory != 0 {
		t.Fatal("mallory is in the project's directory")
	}
	if people, _ := svc.ListWorkflowUsers(own, projectID, 0); len(people) != 1 {
		t.Fatalf("the directory after the refused import: %v", people)
	}
}
