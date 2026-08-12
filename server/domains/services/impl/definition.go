package impl

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/domains/validation"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type definitionService struct {
	repo repositories.Repository
}

// NewDefinitionService creates a new DefinitionService implementation.
func NewDefinitionService(repo repositories.Repository) servicecontracts.DefinitionService {
	return &definitionService{
		repo: repo,
	}
}

func (s *definitionService) CreateDefinition(ctx context.Context, def *entities.ProcessDefinition) (uuid.UUID, error) {
	// Use Visitor Pattern to validate definition
	validator := validation.NewVisitor()
	def.Accept(validator)
	if !validator.IsValid() {
		return uuid.Nil, fmt.Errorf("invalid definition: %s", strings.Join(validator.Errors(), "; "))
	}

	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Versioning logic
		m, err := s.repo.Definition().GetByKey(txCtx, def.Key)
		if err == nil {
			def.Version = m.Version + 1
		} else {
			def.Version = 1
		}

		if def.ID == uuid.Nil {
			idObj, _ := uuid.NewV7()
			def.ID = idObj
		}
		return s.repo.Definition().Create(txCtx, adapters.DefinitionModelAdapter{Definition: def}.ToModel())
	})

	if err != nil {
		return uuid.Nil, err
	}
	return def.ID, nil
}

func (s *definitionService) DeleteDefinition(ctx context.Context, id uuid.UUID) error {
	return s.repo.Definition().Delete(ctx, id)
}

func (s *definitionService) ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error) {
	var ms []models.ProcessDefinitionModel
	var err error
	if projectID != uuid.Nil {
		ms, err = s.repo.Definition().ListByProject(ctx, projectID)
	} else {
		ms, err = s.repo.Definition().List(ctx)
	}
	if err != nil {
		return nil, err
	}
	res := make([]*entities.ProcessDefinition, len(ms))
	for i, m := range ms {
		res[i] = adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *definitionService) GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error) {
	m, err := s.repo.Definition().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return adapters.DefinitionEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *definitionService) GetDefinitionByKey(ctx context.Context, key string) (*entities.ProcessDefinition, error) {
	m, err := s.repo.Definition().GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	return adapters.DefinitionEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *definitionService) ExportDefinition(ctx context.Context, id uuid.UUID) ([]byte, error) {
	def, err := s.GetDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	parser := &BPMNXMLParser{}
	return parser.Export(def)
}

func (s *definitionService) ImportDefinition(ctx context.Context, xmlContent []byte) (uuid.UUID, error) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(bytes.NewReader(xmlContent))
	if err != nil {
		return uuid.Nil, err
	}
	return s.CreateDefinition(ctx, def)
}
