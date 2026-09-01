package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

type formService struct {
	repo repositories.Repository
}

func NewFormService(repo repositories.Repository) servicecontracts.FormService {
	return &formService{repo: repo}
}

func (s *formService) CreateForm(ctx context.Context, projectID uuid.UUID, key, name string, schema map[string]any) (entities.Form, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return entities.Form{}, fmt.Errorf("could not generate a form id: %w", err)
	}
	form := entities.Form{
		ID:        id,
		Project:   &entities.Project{ID: projectID},
		Key:       key,
		Name:      name,
		Schema:    schema,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Form().Create(ctx, adapters.FormModelAdapter{Form: form}.ToModel()); err != nil {
		return entities.Form{}, err
	}
	return form, nil
}

func (s *formService) GetForm(ctx context.Context, id uuid.UUID) (entities.Form, error) {
	m, err := s.repo.Form().Get(ctx, id)
	if err != nil {
		return entities.Form{}, err
	}
	return adapters.FormEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *formService) GetFormByKey(ctx context.Context, projectID uuid.UUID, key string) (entities.Form, error) {
	m, err := s.repo.Form().GetByKey(ctx, projectID, key)
	if err != nil {
		return entities.Form{}, err
	}
	return adapters.FormEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *formService) ListForms(ctx context.Context, projectID uuid.UUID) ([]entities.Form, error) {
	ms, err := s.repo.Form().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Form, len(ms))
	for i, m := range ms {
		res[i] = adapters.FormEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *formService) DeleteForm(ctx context.Context, id uuid.UUID) error {
	return s.repo.Form().Delete(ctx, id)
}
