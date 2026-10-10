package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// seedOCELProject is a project with instances of one definition, each with
// the given number of audit entries.
func seedOCELProject(t *testing.T, repo repositories.Repository, instances, entriesEach int) (context.Context, uuid.UUID) {
	t.Helper()
	projectID, ctx := seedProject(t, repo)
	definitionID := uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(ctx, models.ProcessDefinitionModel{
		Base:      models.Base{ID: models.FromUUID(definitionID)},
		ProjectID: models.FromUUID(projectID),
		Key:       "claims",
		Name:      "Claims",
		Version:   1,
	}); err != nil {
		t.Fatalf("seed the definition: %v", err)
	}
	for range instances {
		instanceID := uuid.Must(uuid.NewV7())
		if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
			Base:         models.Base{ID: models.FromUUID(instanceID)},
			ProjectID:    models.FromUUID(projectID),
			DefinitionID: models.FromUUID(definitionID),
			Status:       models.ProcessActive,
		}); err != nil {
			t.Fatalf("seed an instance: %v", err)
		}
		for range entriesEach {
			if err := repo.Audit().Create(ctx, models.AuditModel{
				ProjectID:  models.FromUUID(projectID),
				InstanceID: models.FromUUID(instanceID),
				Type:       entities.EventNodeReached,
				NodeID:     "review",
				NodeName:   "Review claim",
				Message:    "reached review",
			}); err != nil {
				t.Fatalf("seed an audit entry: %v", err)
			}
		}
	}
	return ctx, projectID
}

// An OCEL export is built whole in memory, so a project with more history
// than one export may hold is refused, with the limit in the message, rather
// than read in full first and the replica's memory with it.
func TestAnOCELExportPastTheLimitIsRefusedAndSaysWhy(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, projectID := seedOCELProject(t, repo, 1, 4)
	engine := NewExecutionEngine(repo, observersimpl.NewEventDispatcher())

	t.Run("one entry more than the limit is refused", func(t *testing.T) {
		_, err := engine.exportOCEL(ctx, projectID, entities.OCELOptions{}, 3)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("export of 4 entries with a limit of 3: err = %v; want a refusal the caller is told about (400)", err)
		}
		if !strings.Contains(err.Error(), "at most 3 audit entries") {
			t.Fatalf("the refusal %q does not say what the limit is", err)
		}
	})

	t.Run("exactly the limit is exported in full", func(t *testing.T) {
		log, err := engine.exportOCEL(ctx, projectID, entities.OCELOptions{}, 4)
		if err != nil {
			t.Fatalf("export of 4 entries with a limit of 4: %v", err)
		}
		if len(log.Events) != 4 {
			t.Fatalf("the export holds %d events; want all 4", len(log.Events))
		}
	})
}

// The instances are read whole as well, variables and all, so more of them
// than the limit is refused before any is read.
func TestAnOCELExportOfMoreInstancesThanTheLimitIsRefused(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, projectID := seedOCELProject(t, repo, 3, 1)
	engine := NewExecutionEngine(repo, observersimpl.NewEventDispatcher())

	_, err := engine.exportOCEL(ctx, projectID, entities.OCELOptions{}, 2)
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("export of 3 instances with a limit of 2: err = %v; want a refusal (400)", err)
	}
	if !strings.Contains(err.Error(), "3 instances") {
		t.Fatalf("the refusal %q does not say how many instances there are", err)
	}
}
