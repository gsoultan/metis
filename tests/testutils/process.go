package testutils

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Process is a running instance of a definition with no steps, in a project
// of an organization of its own: what a job, an incident, a task or an
// external task has to belong to now that each names its instance and
// definition by a key the database holds it to.
type Process struct {
	// Tenant is a caller inside the organization.
	Tenant       context.Context
	Organization uuid.UUID
	Project      uuid.UUID
	Definition   uuid.UUID
	Instance     uuid.UUID
}

// SeedProcess leaves a Process in repo's database.
func SeedProcess(t *testing.T, repo repositories.Repository) Process {
	t.Helper()
	tenant, organization, project := ScopedProject(t, repo)
	p := ProcessIn(t, repo, tenant, project)
	p.Organization = organization
	return p
}

// Another leaves one more instance of p's definition, in status.
func (p Process) Another(t *testing.T, repo repositories.Repository, status models.ProcessStatus) uuid.UUID {
	t.Helper()
	id, err := repo.Process().Create(p.Tenant, models.ProcessInstanceModel{
		Base: models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))}, ProjectID: models.UUID(p.Project),
		DefinitionID: models.UUID(p.Definition), Status: status,
	})
	if err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	return id
}

// ProcessIn is SeedProcess in a project that already exists, created as the
// caller of tenant: for a test that has its own organization and project.
func ProcessIn(t *testing.T, repo repositories.Repository, tenant context.Context, project uuid.UUID) Process {
	t.Helper()
	p := Process{Tenant: tenant, Project: project, Definition: uuid.Must(uuid.NewV7())}
	if err := repo.Definition().Create(tenant, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.UUID(p.Definition)}, ProjectID: models.UUID(project),
		Key: "seeded-" + p.Definition.String()[:8], Name: "Seeded", Version: 1,
	}); err != nil {
		t.Fatalf("seed the definition: %v", err)
	}
	p.Instance = p.Another(t, repo, models.ProcessActive)
	return p
}
