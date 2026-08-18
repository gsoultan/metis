package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	"time"
)

type deploymentService struct {
	repo       repositories.Repository
	defService servicecontracts.DefinitionService
}

func NewDeploymentService(repo repositories.Repository, defService servicecontracts.DefinitionService) servicecontracts.DeploymentService {
	return &deploymentService{
		repo:       repo,
		defService: defService,
	}
}

func (s *deploymentService) Deploy(ctx context.Context, projectID uuid.UUID, name string, resources []entities.Resource) (entities.Deployment, error) {
	var deployment entities.Deployment
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		deploymentID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("could not generate a deployment id: %w", err)
		}
		deployment = entities.Deployment{
			ID:        deploymentID,
			Project:   &entities.Project{ID: projectID},
			Name:      name,
			CreatedAt: time.Now(),
			Resources: resources,
		}

		for i := range deployment.Resources {
			if deployment.Resources[i].ID == uuid.Nil {
				resourceID, err := uuid.NewV7()
				if err != nil {
					return fmt.Errorf("could not generate an id for resource %q: %w", deployment.Resources[i].Name, err)
				}
				deployment.Resources[i].ID = resourceID
			}
			deployment.Resources[i].Deployment = &deployment
		}

		// CreateAuditEntry Deployment and Resources in repository
		if err := s.repo.Deployment().Create(txCtx, adapters.DeploymentModelAdapter{Deployment: deployment}.ToModel()); err != nil {
			return err
		}

		// For each resource, try to parse and create process definition
		for _, res := range resources {
			if res.Type == "BPMN_JSON" || res.Type == "JSON" {
				var def *entities.ProcessDefinition
				if err := json.Unmarshal(res.Content, &def); err != nil {
					continue // Skip if not a valid process definition
				}
				def.Deployment = &deployment
				def.Project = deployment.Project
				// Ensure it has an ID if missing
				if def.ID == uuid.Nil {
					definitionID, err := uuid.NewV7()
					if err != nil {
						return fmt.Errorf("could not generate an id for the definition in %q: %w", res.Name, err)
					}
					def.ID = definitionID
				}
				if _, err := s.defService.CreateDefinition(txCtx, def); err != nil {
					return fmt.Errorf("failed to create definition from resource %s: %w", res.Name, err)
				}
			}
		}
		return nil
	})

	if err != nil {
		return entities.Deployment{}, err
	}

	return deployment, nil
}

func (s *deploymentService) GetDeployment(ctx context.Context, id uuid.UUID) (entities.Deployment, error) {
	m, err := s.repo.Deployment().Get(ctx, id)
	if err != nil {
		return entities.Deployment{}, err
	}
	return adapters.DeploymentEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *deploymentService) ListDeployments(ctx context.Context, projectID uuid.UUID) ([]entities.Deployment, error) {
	ms, err := s.repo.Deployment().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Deployment, len(ms))
	for i, m := range ms {
		res[i] = adapters.DeploymentEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}
