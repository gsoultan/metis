package impl

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

// maxSimulationCaseBytes bounds one case as it is stored.
//
// A case is read whole every time the designer opens its process, as are all
// twenty of its neighbours. Sixty-four kilobytes is far more variables and
// answers than a case is written with, and twenty of them is still a small
// read.
const maxSimulationCaseBytes = 64 << 10

type simulationCaseService struct {
	repo repositories.Repository
}

// NewSimulationCaseService returns the service that keeps processes'
// simulation test cases.
func NewSimulationCaseService(repo repositories.Repository) contracts.SimulationCaseService {
	return &simulationCaseService{repo: repo}
}

func (s *simulationCaseService) ListSimulationCases(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.SimulationCase, error) {
	key, err := periodProcessKey(processKey)
	if err != nil {
		return nil, err
	}
	return s.repo.SimulationCase().ListByProcess(ctx, projectID, key)
}

func (s *simulationCaseService) SaveSimulationCase(ctx context.Context, c entities.SimulationCase) (entities.SimulationCase, error) {
	if err := checkSimulationCase(&c); err != nil {
		return entities.SimulationCase{}, err
	}
	if c.ID == uuid.Nil {
		return s.repo.SimulationCase().Create(ctx, c)
	}
	return s.repo.SimulationCase().Update(ctx, c)
}

func (s *simulationCaseService) DeleteSimulationCase(ctx context.Context, id uuid.UUID) error {
	return s.repo.SimulationCase().Delete(ctx, id)
}

// checkSimulationCase refuses a case that could never be run or checked, and
// trims what it keeps.
//
// Refused at save rather than at run, because a case is saved once and run
// many times: an answer of a kind nobody can give, or an expectation of an
// outcome no process reaches, would otherwise fail every run after it with a
// message about a case written long ago.
func checkSimulationCase(c *entities.SimulationCase) error {
	key, err := periodProcessKey(c.ProcessKey)
	if err != nil {
		return err
	}
	c.ProcessKey = key

	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > entities.MaxSimulationCaseNameLength {
		return apierr.Invalidf("give the case a name of at most %d characters", entities.MaxSimulationCaseNameLength)
	}
	if err := checkCaseAnswers(c.Scenario.Answers); err != nil {
		return err
	}
	if !entities.IsExpectable(c.Expect.Outcome) {
		return apierr.Invalidf("a case can expect a run to complete, to stop for an answer, or to raise an incident — not %q",
			c.Expect.Outcome)
	}

	encoded, err := json.Marshal(struct {
		Scenario entities.SimulationScenario
		Expect   entities.SimulationExpectation
	}{c.Scenario, c.Expect})
	if err != nil {
		return apierr.Invalidf("the case's values could not be stored: %v", err)
	}
	if len(encoded) > maxSimulationCaseBytes {
		return apierr.Invalidf("this case is %d bytes; a case keeps at most %d", len(encoded), maxSimulationCaseBytes)
	}
	return nil
}

// checkCaseAnswers refuses an answer of an unknown kind, or two for one step.
func checkCaseAnswers(answers []entities.SimulationAnswer) error {
	seen := make(map[string]bool, len(answers))
	for _, answer := range answers {
		switch answer.Kind {
		case entities.AnswerPerson, entities.AnswerService, entities.AnswerMessage:
		default:
			return apierr.Invalidf("the answer for %q is of kind %q, which no step asks for", answer.Node, answer.Kind)
		}
		if answer.Node == "" {
			return apierr.Invalidf("every answer names the step it answers")
		}
		if seen[answer.Node] {
			return apierr.Invalidf("the step %q is answered twice", answer.Node)
		}
		seen[answer.Node] = true
	}
	return nil
}
