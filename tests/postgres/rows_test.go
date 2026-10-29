package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
)

// rewriteInstance changes an instance's row behind the repository's back: to
// make it older than a sweep's cut-off, or a child of another.
func rewriteInstance(ctx context.Context, t *testing.T, conn *db.Conn, id uuid.UUID, change func(*processinstance.Mut)) {
	t.Helper()
	mut := processinstance.MutateKey(id)
	change(&mut)
	if err := mut.Update(ctx, testutils.Executor(conn)); err != nil {
		t.Fatalf("rewrite instance %s: %v", id, err)
	}
}

// ageInstance makes an instance last change by ago.
func ageInstance(ctx context.Context, t *testing.T, conn *db.Conn, id uuid.UUID, by time.Duration) {
	t.Helper()
	rewriteInstance(ctx, t, conn, id, func(m *processinstance.Mut) { m.SetUpdatedAt(time.Now().Add(-by)) })
}

// caseIn leaves a definition and an active instance of it in project, for the
// rows a test needs only as something that belongs to a case: a job, an
// external task, an incident. The schema refuses them without one.
func caseIn(ctx context.Context, t *testing.T, repo repositories.Repository, project uuid.UUID) (definition, instance uuid.UUID) {
	t.Helper()
	ctx = entities.WithSystemContext(ctx)
	definition = uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(ctx, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.UUID(definition)}, ProjectID: models.UUID(project),
		Key: "case-" + definition.String(), Name: "case", Version: 1,
	}); err != nil {
		t.Fatalf("seed a definition: %v", err)
	}
	instance, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base: models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))}, ProjectID: models.UUID(project),
		DefinitionID: models.UUID(definition), Status: models.ProcessActive,
	})
	if err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	return definition, instance
}

// aCase is caseIn a project of a new organization of its own.
func aCase(t *testing.T, repo repositories.Repository) (definition, instance uuid.UUID) {
	t.Helper()
	_, _, project := testutils.ScopedProject(t, repo)
	return caseIn(t.Context(), t, repo, project)
}
