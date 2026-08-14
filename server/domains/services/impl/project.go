package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type projectService struct {
	repo repositories.Repository
}

// NewProjectService creates a new ProjectService implementation.
func NewProjectService(
	repo repositories.Repository,
) servicecontracts.ProjectService {
	return &projectService{
		repo: repo,
	}
}

func (s *projectService) CreateProject(ctx context.Context, organizationID uuid.UUID, name, description string) (entities.Project, error) {
	if name == "" {
		return entities.Project{}, errors.New("project name is required")
	}
	if organizationID == uuid.Nil {
		return entities.Project{}, errors.New("organization ID is required")
	}
	idObj, err := uuid.NewV7()
	if err != nil {
		return entities.Project{}, fmt.Errorf("could not generate UUID: %w", err)
	}
	p := entities.Project{
		ID:           idObj,
		Organization: &entities.Organization{ID: organizationID},
		Name:         name,
		Description:  description,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.repo.Project().Create(ctx, adapters.ProjectModelAdapter{Project: p}.ToModel()); err != nil {
		return entities.Project{}, err
	}
	return p, nil
}

func (s *projectService) GetProject(ctx context.Context, id uuid.UUID) (entities.Project, error) {
	m, err := s.repo.Project().Get(ctx, id)
	if err != nil {
		return entities.Project{}, err
	}
	return adapters.ProjectEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *projectService) ListProjects(ctx context.Context, organizationID uuid.UUID) ([]entities.Project, error) {
	ms, err := s.repo.Project().ListByOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Project, len(ms))
	for i, m := range ms {
		res[i] = adapters.ProjectEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *projectService) UpdateProject(ctx context.Context, id uuid.UUID, organizationID uuid.UUID, name, description string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Project().Get(txCtx, id)
		if err != nil {
			return err
		}
		if organizationID != uuid.Nil {
			m.OrganizationID = models.UUID(organizationID)
		}
		if name != "" {
			m.Name = name
		}
		m.Description = description
		m.UpdatedAt = time.Now()
		return s.repo.Project().Update(txCtx, m)
	})
}

func (s *projectService) DeleteProject(ctx context.Context, id uuid.UUID) error {
	return s.repo.Project().Delete(ctx, id)
}

func (s *projectService) GetProcessStatistics(ctx context.Context, projectID uuid.UUID) (entities.ProcessStatistics, error) {
	active, _ := s.repo.Process().CountByStatus(ctx, projectID, models.ProcessActive)
	completed, _ := s.repo.Process().CountByStatus(ctx, projectID, models.ProcessCompleted)
	failed, _ := s.repo.Process().CountByStatus(ctx, projectID, models.ProcessFailed)

	totalTasks, _ := s.repo.Task().CountByStatus(ctx, projectID, "")
	pendingTasks, _ := s.repo.Task().CountByStatus(ctx, projectID, models.TaskUnclaimed)

	nodeFreqs := make(map[string]int)
	if projectID != uuid.Nil {
		ms, _ := s.repo.Audit().ListByProject(ctx, projectID)
		for _, m := range ms {
			if m.NodeID != "" {
				nodeFreqs[m.NodeID]++
			}
		}
	}

	return entities.ProcessStatistics{
		ActiveInstances:    int(active),
		CompletedInstances: int(completed),
		FailedInstances:    int(failed),
		TotalTasks:         int(totalTasks),
		PendingTasks:       int(pendingTasks),
		NodeFrequencies:    nodeFreqs,
	}, nil
}
