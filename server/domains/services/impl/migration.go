package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/repositories/models"

	"github.com/google/uuid"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
)

type migrationService struct {
	repo repositories.Repository
}

// NewMigrationService creates a new MigrationService implementation.
func NewMigrationService(
	repo repositories.Repository,
) servicecontracts.MigrationService {
	return &migrationService{
		repo: repo,
	}
}

func (s *migrationService) MigrateInstances(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string) error {
	instances, err := s.repo.Process().ListByDefinition(ctx, sourceDefID)
	if err != nil {
		return fmt.Errorf("failed to list instances for migration: %w", err)
	}

	for _, instance := range instances {
		err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
			// UpdateConnectorInstance Process Instance
			instance.DefinitionID = models.UUID(targetDefID)
			for i := range instance.Tokens {
				if newNodeID, ok := nodeMapping[instance.Tokens[i].NodeID]; ok {
					instance.Tokens[i].NodeID = newNodeID
				}
			}
			if err := s.repo.Process().Update(txCtx, instance); err != nil {
				return err
			}

			// UpdateConnectorInstance Tasks
			tasks, err := s.repo.Task().ListByInstance(txCtx, uuid.UUID(instance.ID))
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if newNodeID, ok := nodeMapping[task.NodeID]; ok {
					task.NodeID = newNodeID
					if err := s.repo.Task().Update(txCtx, task); err != nil {
						return err
					}
				}
			}

			// UpdateConnectorInstance Jobs
			jobs, err := s.repo.Job().ListByInstance(txCtx, uuid.UUID(instance.ID))
			if err != nil {
				return err
			}
			for _, job := range jobs {
				job.DefinitionID = models.UUID(targetDefID)
				if newNodeID, ok := nodeMapping[job.NodeID]; ok {
					job.NodeID = newNodeID
				}
				if err := s.repo.Job().Update(txCtx, job); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to migrate instance %s: %w", instance.ID, err)
		}
	}

	return nil
}
