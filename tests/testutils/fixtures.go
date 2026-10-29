package testutils

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/metis/server/repositories/store/webhook"
	"github.com/gsoultan/storm/runtime"
)

// SeedDefinition leaves a process definition with no steps in project, as
// version 1 of key, and returns its id.
//
// The rows a test needs only as something to point at: a job, an incident or
// an instance has to name a definition that exists.
func SeedDefinition(t *testing.T, conn *db.Conn, project uuid.UUID, key string) uuid.UUID {
	t.Helper()
	return SeedDefinitionVersion(t.Context(), t, conn, project, key, 1)
}

// SeedDefinitionVersion is SeedDefinition at a version of the caller's choosing.
func SeedDefinitionVersion(ctx context.Context, t *testing.T, conn *db.Conn, project uuid.UUID, key string, version int) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	row := processdefinition.Create()
	row.SetID(id)
	row.SetProjectID(project)
	row.SetKey(key)
	row.SetName(key)
	row.SetVersion(int64(version))
	row.SetNodes(runtime.JSON("[]"))
	row.SetFlows(runtime.JSON("[]"))
	if _, err := row.Insert(ctx, Executor(conn)); err != nil {
		t.Fatalf("seed the definition %s: %v", key, err)
	}
	return id
}

// SeedInstance leaves an instance of definition in project, in status, and
// returns its id.
func SeedInstance(t *testing.T, conn *db.Conn, project, definition uuid.UUID, status models.ProcessStatus) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	row := processinstance.Create()
	row.SetID(id)
	row.SetProjectID(project)
	row.SetDefinitionID(definition)
	row.SetStatus(string(status))
	row.SetVariables("")
	row.SetTokens(runtime.JSON("[]"))
	row.SetCompletedNodes(runtime.JSON("[]"))
	row.SetCompensatedNodes(runtime.JSON("[]"))
	row.SetMultiInstance(runtime.JSON("{}"))
	row.SetJoins(runtime.JSON("{}"))
	if _, err := row.Insert(t.Context(), Executor(conn)); err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	return id
}

// SeedWebhook leaves an enabled inbound webhook in project and returns its id:
// what a recorded delivery has to belong to.
func SeedWebhook(t *testing.T, conn *db.Conn, project uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	row := webhook.Create()
	row.SetID(id)
	row.SetProjectID(project)
	row.SetName("hook " + id.String()[:8])
	row.SetToken(uuid.NewString())
	row.SetSecret("")
	row.SetSignatureHeader("")
	row.SetMessageName("hook")
	row.SetEnabled(true)
	if _, err := row.Insert(t.Context(), Executor(conn)); err != nil {
		t.Fatalf("seed a webhook: %v", err)
	}
	return id
}

// SeedProject leaves a project, in an organization of its own, in conn's main
// database and returns the project's id. Written through the stores, so it
// asks nothing of the repository layer: for a database a test opened itself.
func SeedProject(t *testing.T, conn *db.Conn) uuid.UUID {
	t.Helper()
	ex := Executor(conn)
	org := organization.Create()
	org.SetName("Seeded")
	org.SetDescription("")
	written, err := org.Insert(t.Context(), ex)
	if err != nil {
		t.Fatalf("seed an organization: %v", err)
	}
	id := uuid.Must(uuid.NewV7())
	row := project.Create()
	row.SetID(id)
	row.SetOrganizationID(written.ID)
	row.SetName("Seeded")
	row.SetDescription("")
	if _, err := row.Insert(t.Context(), ex); err != nil {
		t.Fatalf("seed a project: %v", err)
	}
	return id
}
