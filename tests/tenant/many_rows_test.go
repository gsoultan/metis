package tenant

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	projectstore "github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/metis/tests/testutils"
)

// seedProjects leaves n projects in org, named in order, in one COPY.
func seedProjects(t *testing.T, conn *db.Conn, org uuid.UUID, n int) {
	t.Helper()
	now := time.Now()
	rows := make([]projectstore.Row, n)
	for i := range rows {
		rows[i] = projectstore.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now,
			OrganizationID: org, Name: fmt.Sprintf("project-%05d", i+1)}
	}
	if _, err := projectstore.InsertAll(t.Context(), testutils.Executor(conn), rows); err != nil {
		t.Fatalf("seed %d projects: %v", n, err)
	}
}

// seedOrganizations leaves n organizations, named in order, in one COPY.
func seedOrganizations(t *testing.T, conn *db.Conn, n int) {
	t.Helper()
	now := time.Now()
	rows := make([]organization.Row, n)
	for i := range rows {
		rows[i] = organization.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now,
			Name: fmt.Sprintf("organization-%05d", i+1)}
	}
	if _, err := organization.InsertAll(t.Context(), testutils.Executor(conn), rows); err != nil {
		t.Fatalf("seed %d organizations: %v", n, err)
	}
}
