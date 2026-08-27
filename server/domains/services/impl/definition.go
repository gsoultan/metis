package impl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/domains/validation"
	"github.com/gsoultan/gobpm/server/repositories"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
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

	if def.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("could not generate a definition id: %w", err)
		}
		def.ID = id
	}

	// The version series is per project, matching the unique index, so the
	// allocator needs the same project the adapter is about to write.
	var projectID uuid.UUID
	if def.Project != nil {
		projectID = def.Project.ID
	}

	err := allocateVersion(ctx, s.repo.UnitOfWork(), "process "+def.Key,
		func(ctx context.Context) (int, error) {
			return s.repo.Definition().NextVersion(ctx, projectID, def.Key)
		},
		func(txCtx context.Context, version int) error {
			def.Version = version
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

// ListDefinitionsPaged returns one page of a project's definitions.
//
// The unpaged call returns every version of every process the project has ever
// had. That list only grows, and it exists to pick one process from.
func (s *definitionService) ListDefinitionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[*entities.ProcessDefinition], error) {
	result, err := s.repo.Definition().ListByProjectPaged(ctx, projectID, page)
	if err != nil {
		return repocontracts.Page[*entities.ProcessDefinition]{}, err
	}
	defs := make([]*entities.ProcessDefinition, len(result.Items))
	for i, m := range result.Items {
		def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
		defs[i] = def
	}
	return repocontracts.NewPage(defs, result.Total, page), nil
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

// ImportDefinition deploys BPMN XML into a project.
//
// The project is required, not optional: definitions are looked up under the
// caller's tenant scope, which joins through the project. An import that set no
// project produced a definition the tenant scope could never find — deployed,
// versioned, and permanently invisible to the organization that uploaded it.
func (s *definitionService) ImportDefinition(ctx context.Context, projectID uuid.UUID, xmlContent []byte) (uuid.UUID, error) {
	if projectID == uuid.Nil {
		return uuid.Nil, errors.New("import requires a project_id: a definition without a project is invisible to its own organization")
	}
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(bytes.NewReader(xmlContent))
	if err != nil {
		return uuid.Nil, err
	}
	def.Project = &entities.Project{ID: projectID}
	return s.CreateDefinition(ctx, def)
}

// ListJavaScriptConditions walks every definition the caller can see and
// reports each condition the evaluator chain would hand to JavaScript. This is
// the worklist for the javascript-conditions flag: it ships off, so anything
// reported here is a decision point that will refuse to route until rewritten
// in FEEL. It is deliberately complete rather than paged — a truncated worklist
// reads as "migration done" when it is not — so the scan is batched instead,
// holding one batch of graphs at a time rather than the whole installation.
func (s *definitionService) ListJavaScriptConditions(ctx context.Context) ([]entities.JavaScriptConditionUsage, error) {
	usages := make([]entities.JavaScriptConditionUsage, 0)
	err := s.repo.Definition().ScanWithGraphs(ctx, func(batch []models.ProcessDefinitionModel) error {
		for _, m := range batch {
			def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
			usages = append(usages, collectJavaScriptConditions(def)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return usages, nil
}

// collectJavaScriptConditions finds the `js:` conditions in one definition.
//
// The prefix check mirrors JSExpressionEvaluator exactly — HasPrefix, no
// trimming — because this report is a promise about what that evaluator will
// refuse. A condition it would not treat as JavaScript must not appear here.
func collectJavaScriptConditions(def *entities.ProcessDefinition) []entities.JavaScriptConditionUsage {
	c := &jsConditionCollector{def: def}
	def.Accept(c)
	return c.usages
}

// jsConditionCollector is the DefinitionVisitor behind
// collectJavaScriptConditions. It inspects the two fields the condition chain
// evaluates: sequence-flow conditions and completion conditions. Node.Condition
// is deliberately ignored — it carries timer expressions and script bodies,
// which the javascript-conditions flag does not gate, and reporting them would
// make the worklist lie about what turning the flag on or off changes.
type jsConditionCollector struct {
	def    *entities.ProcessDefinition
	usages []entities.JavaScriptConditionUsage
}

func (c *jsConditionCollector) VisitDefinition(*entities.ProcessDefinition) {}

func (c *jsConditionCollector) VisitFlowNode(n *entities.Node) {
	if n == nil {
		return
	}
	c.record(n.ID, n.Name, "completion condition", n.CompletionCondition)
}

func (c *jsConditionCollector) VisitSequenceFlow(sf *entities.SequenceFlow) {
	if sf == nil {
		return
	}
	c.record(sf.ID, "", "flow condition", sf.Condition)
}

func (c *jsConditionCollector) record(elementID, elementName, where, condition string) {
	if !strings.HasPrefix(condition, "js:") {
		return
	}
	c.usages = append(c.usages, entities.JavaScriptConditionUsage{
		DefinitionID:   c.def.ID,
		DefinitionKey:  c.def.Key,
		DefinitionName: c.def.Name,
		Version:        c.def.Version,
		ElementID:      elementID,
		ElementName:    elementName,
		Where:          where,
		Condition:      condition,
	})
}
