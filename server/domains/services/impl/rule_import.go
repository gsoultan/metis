package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/grule"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

// decisionCreator and processDeployer are what an import stores through: the
// same calls a person saving a table or deploying a process makes, so an
// imported table is checked and versioned exactly as one written by hand.
type decisionCreator interface {
	CreateDecision(ctx context.Context, def entities.DecisionDefinition) (uuid.UUID, error)
}

type processDeployer interface {
	DeployDefinition(ctx context.Context, def *entities.ProcessDefinition, promote bool) (uuid.UUID, error)
}

type ruleImportService struct {
	repo        repositories.Repository
	decisions   decisionCreator
	definitions processDeployer
}

// NewRuleImportService creates the service that imports Grule rules.
func NewRuleImportService(repo repositories.Repository, decisions decisionCreator, definitions processDeployer) contracts.RuleImporter {
	return &ruleImportService{repo: repo, decisions: decisions, definitions: definitions}
}

func (s *ruleImportService) ImportGruleRules(ctx context.Context, req entities.GruleImportRequest) (entities.GruleImport, error) {
	if req.ProjectID == uuid.Nil {
		return entities.GruleImport{}, apierr.Invalidf("an import needs a project_id")
	}
	// Read first, so that neither a preview nor an import says anything about
	// a project the caller cannot see, such as which keys it already uses.
	if _, err := s.repo.Project().Get(ctx, req.ProjectID); err != nil {
		return entities.GruleImport{}, err
	}
	result, err := translateGrule(req)
	if err != nil {
		return entities.GruleImport{}, err
	}
	taken, err := s.takenKeys(ctx, req.ProjectID, result)
	if err != nil {
		return entities.GruleImport{}, err
	}
	if req.Preview {
		for _, key := range taken {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s is already used in this project; choose another key to import", key))
		}
		return result, nil
	}
	if len(taken) > 0 {
		// Never a new version of an existing key: other processes consult
		// that key, and an import would change their policy underneath them.
		return entities.GruleImport{}, apierr.Invalidf("%s%s already used in this project; choose another key", strings.Join(taken, ", "), pluralIs(taken))
	}
	if len(result.Decisions) == 0 {
		return entities.GruleImport{}, apierr.Invalidf("none of the rules could be imported; the preview says why for each")
	}
	if err := s.store(ctx, req, &result); err != nil {
		return entities.GruleImport{}, err
	}
	return result, nil
}

func translateGrule(req entities.GruleImportRequest) (entities.GruleImport, error) {
	sources := make([]grule.Source, len(req.Files))
	for i, f := range req.Files {
		sources[i] = grule.Source{Name: f.Name, Content: []byte(f.Content)}
	}
	rules, err := grule.ParseSources(sources)
	if err != nil {
		return entities.GruleImport{}, apierr.Invalidf("%s", err.Error())
	}
	result, err := grule.Translate(rules, req.Key, req.Name)
	if err != nil {
		return entities.GruleImport{}, apierr.Invalidf("%s", err.Error())
	}
	return result, nil
}

func pluralIs(keys []string) string {
	if len(keys) == 1 {
		return " is"
	}
	return " are"
}

// takenKeys lists the keys the import would make that the project already
// has, decisions and the process alike.
func (s *ruleImportService) takenKeys(ctx context.Context, projectID uuid.UUID, result entities.GruleImport) ([]string, error) {
	var taken []string
	for _, d := range result.Decisions {
		next, err := s.repo.Decision().NextVersion(ctx, projectID, d.Key)
		if err != nil {
			return nil, fmt.Errorf("could not check whether decision %s exists: %w", d.Key, err)
		}
		if next > 1 {
			taken = append(taken, d.Key)
		}
	}
	if result.Process != nil {
		next, err := s.repo.Definition().NextVersion(ctx, projectID, result.Process.Key)
		if err != nil {
			return nil, fmt.Errorf("could not check whether process %s exists: %w", result.Process.Key, err)
		}
		if next > 1 {
			taken = append(taken, result.Process.Key)
		}
	}
	return taken, nil
}

// store saves every table and, when asked, deploys the process, all or
// nothing: half an import is a process consulting tables that are not there.
func (s *ruleImportService) store(ctx context.Context, req entities.GruleImportRequest, result *entities.GruleImport) error {
	project := &entities.Project{ID: req.ProjectID}
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		for i := range result.Decisions {
			result.Decisions[i].Project = project
			id, err := s.decisions.CreateDecision(txCtx, result.Decisions[i])
			if err != nil {
				return fmt.Errorf("could not store decision %s: %w", result.Decisions[i].Key, err)
			}
			result.Decisions[i].ID = id
		}
		result.Stored = true
		if !req.Deploy || result.Process == nil {
			return nil
		}
		result.Process.Project = project
		id, err := s.definitions.DeployDefinition(txCtx, result.Process, true)
		if err != nil {
			return fmt.Errorf("could not deploy process %s: %w", result.Process.Key, err)
		}
		result.ProcessID = id.String()
		return nil
	})
}
