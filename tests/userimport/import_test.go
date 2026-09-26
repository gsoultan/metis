package userimport_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/compile/pgddl"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fixture builds the whole path — connection, repository, service — against an
// isolated schema in a real PostgreSQL.
//
// A schema per test rather than a shared one: these write, and a test that
// inherits another's participants passes for the wrong reason.
func fixture(t *testing.T) (context.Context, *impl.WorkflowUserServiceForTest, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("STORM_DSN")
	if dsn == "" {
		t.Skip("STORM_DSN unset")
	}
	ctx := context.Background()

	namespace := fmt.Sprintf("imp_%s", strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer admin.Close()

	schema, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+namespace); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+namespace+" CASCADE")
	})
	if _, err := admin.Exec(ctx, "SET search_path TO "+namespace+"; "+pgddl.Create(schema)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	conn, err := db.Open(ctx, dsn+"&search_path="+namespace)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(conn.Close)

	var orgID, projectID uuid.UUID
	if err := conn.Main().QueryRow(ctx,
		"INSERT INTO organizations (name) VALUES ('Acme') RETURNING id").Scan(&orgID); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	if err := conn.Main().QueryRow(ctx,
		"INSERT INTO projects (organization_id, name) VALUES ($1, 'Payments') RETURNING id", orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	svc := impl.NewWorkflowUserService(pg.NewWorkflowUserRepository(conn))
	lastConn = conn
	return ctx, &impl.WorkflowUserServiceForTest{WorkflowUserService: svc}, projectID
}

// lastConn is the connection the most recent fixture opened.
//
// A package variable rather than a return value because fixture already returns
// three things and every caller but the sync tests would ignore a fourth. Tests
// in one package run in sequence unless they say otherwise, and these do not.
var lastConn *db.Conn

func connOf(t *testing.T) *db.Conn {
	t.Helper()
	if lastConn == nil {
		t.Fatal("no connection; call fixture first")
	}
	return lastConn
}

// A directory imports, with its groups created along the way.
func TestImportingADirectory(t *testing.T) {
	ctx, svc, projectID := fixture(t)

	summary, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader(
		`username,display_name,email,groups,active
ada,Ada Lovelace,ada@example.com,approvers;finance,yes
bob,Bob Vance,bob@example.com,approvers,yes
carol,Carol Danvers,carol@example.com,,no
`))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Created != 3 || summary.Updated != 0 {
		t.Fatalf("expected three new participants, got %+v", summary)
	}
	if summary.Groups != 2 {
		t.Fatalf("the file names two groups, got %d", summary.Groups)
	}
	if len(summary.Problems) != 0 {
		t.Fatalf("expected no problems, got %v", summary.Problems)
	}

	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(people) != 3 {
		t.Fatalf("expected three participants, got %d", len(people))
	}
	// Ordered by name, and the inactive one is still there.
	if people[0].Username != "ada" || people[2].Username != "carol" || people[2].Active {
		t.Errorf("read back wrongly: %+v", people)
	}
	// A CSV carries names, not passwords: nobody can sign in yet.
	for _, p := range people {
		if p.HasCredentials {
			t.Errorf("%s should have no credentials from an import", p.Username)
		}
	}
}

// Importing the same file twice updates rather than duplicating, and reports
// which is which.
func TestReimportingUpdatesRatherThanDuplicating(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	const file = "username,display_name\nada,Ada Lovelace\n"

	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader(file)); err != nil {
		t.Fatalf("first import: %v", err)
	}
	summary, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader(
		"username,display_name\nada,Ada L. Byron\n"))
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if summary.Created != 0 || summary.Updated != 1 {
		t.Fatalf("a re-import updates rather than creates, got %+v", summary)
	}

	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(people) != 1 {
		t.Fatalf("expected one participant, got %d", len(people))
	}
	if people[0].DisplayName != "Ada L. Byron" {
		t.Errorf("the second import should have taken effect, got %q", people[0].DisplayName)
	}
}

// A bad row is reported and the rest of the file still lands.
func TestOneBadRowDoesNotLoseTheImport(t *testing.T) {
	ctx, svc, projectID := fixture(t)

	summary, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader(
		`username,email
ada,ada@example.com
bob,not-an-address
carol,carol@example.com
`))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Created != 2 {
		t.Fatalf("the two good rows should import, got %+v", summary)
	}
	if len(summary.Problems) != 1 || summary.Problems[0].Username != "bob" {
		t.Fatalf("the bad row should be reported by name, got %v", summary.Problems)
	}

	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("expected two participants, got %d", len(people))
	}
}

// A file that cannot be used at all is refused as a file.
func TestAnUnusableFileIsRefusedWhole(t *testing.T) {
	ctx, svc, projectID := fixture(t)

	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("email\nada@example.com\n")); err == nil {
		t.Fatal("a file with no username column should be refused")
	}
	people, err := svc.ListWorkflowUsers(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(people) != 0 {
		t.Fatalf("a refused file must import nothing, got %d", len(people))
	}
}
