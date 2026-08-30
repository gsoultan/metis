package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

type organizationService struct {
	repo repositories.Repository
}

// NewOrganizationService creates a new OrganizationService implementation.
func NewOrganizationService(repo repositories.Repository) servicecontracts.OrganizationService {
	return &organizationService{
		repo: repo,
	}
}

func (s *organizationService) CreateOrganization(ctx context.Context, name, description string) (entities.Organization, error) {
	if name == "" {
		return entities.Organization{}, errors.New("organization name is required")
	}
	idObj, err := uuid.NewV7()
	if err != nil {
		return entities.Organization{}, fmt.Errorf("could not generate UUID: %w", err)
	}
	o := entities.Organization{
		ID:          idObj,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.repo.Organization().Create(ctx, adapters.OrganizationModelAdapter{Organization: o}.ToModel()); err != nil {
		return entities.Organization{}, err
	}
	return o, nil
}

func (s *organizationService) GetOrganization(ctx context.Context, id uuid.UUID) (entities.Organization, error) {
	m, err := s.repo.Organization().Get(ctx, id)
	if err != nil {
		return entities.Organization{}, err
	}
	return adapters.OrganizationEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *organizationService) ListOrganizations(ctx context.Context) ([]entities.Organization, error) {
	ms, err := s.repo.Organization().List(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Organization, len(ms))
	for i, m := range ms {
		res[i] = adapters.OrganizationEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *organizationService) UpdateOrganization(ctx context.Context, id uuid.UUID, name, description string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Organization().Get(txCtx, id)
		if err != nil {
			return err
		}
		if name != "" {
			m.Name = name
		}
		m.Description = description
		m.UpdatedAt = time.Now()
		return s.repo.Organization().Update(txCtx, m)
	})
}

func (s *organizationService) DeleteOrganization(ctx context.Context, id uuid.UUID) error {
	return s.repo.Organization().Delete(ctx, id)
}
