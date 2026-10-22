package simulationcase

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// Endpoints keep the simulation test cases of each process.
type Endpoints struct {
	ListSimulationCases  endpoint.Endpoint
	SaveSimulationCase   endpoint.Endpoint
	DeleteSimulationCase endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListSimulationCases:  makeListEndpoint(s),
		SaveSimulationCase:   makeSaveEndpoint(s),
		DeleteSimulationCase: makeDeleteEndpoint(s),
	}
}

func makeListEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListCasesRequest)
		if !ok {
			return nil, fmt.Errorf("simulation case: expected a ListCasesRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return ListCasesResponse{Err: err}, nil
		}
		cases, err := s.ListSimulationCases(ctx, projectID, req.ProcessKey)
		if err != nil {
			return ListCasesResponse{Err: err}, nil
		}
		views := make([]CaseView, 0, len(cases))
		for _, c := range cases {
			views = append(views, ViewOf(c))
		}
		return ListCasesResponse{Cases: views}, nil
	}
}

// makeSaveEndpoint creates a case when the address names a project and a
// process, and rewrites one when it also names a case.
func makeSaveEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SaveCaseRequest)
		if !ok {
			return nil, fmt.Errorf("simulation case: expected a SaveCaseRequest, got %T", request)
		}
		c, err := caseOf(req)
		if err != nil {
			return SaveCaseResponse{Err: err}, nil
		}
		saved, err := s.SaveSimulationCase(ctx, c)
		if err != nil {
			return SaveCaseResponse{Err: err}, nil
		}
		view := ViewOf(saved)
		return SaveCaseResponse{Case: &view}, nil
	}
}

func makeDeleteEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteCaseRequest)
		if !ok {
			return nil, fmt.Errorf("simulation case: expected a DeleteCaseRequest, got %T", request)
		}
		id, err := parseID("simulation case", req.CaseID)
		if err != nil {
			return DeleteCaseResponse{Err: err}, nil
		}
		return DeleteCaseResponse{Err: s.DeleteSimulationCase(ctx, id)}, nil
	}
}

// caseOf reads the case a save names, with its project and process from the
// address. A rewrite names the case as well, and the repository refuses one
// whose stored project or process is not the address's: editing a case never
// moves it.
func caseOf(req SaveCaseRequest) (entities.SimulationCase, error) {
	projectID, err := parseID("project", req.ProjectID)
	if err != nil {
		return entities.SimulationCase{}, err
	}
	c := entities.SimulationCase{
		Project:    &entities.Project{ID: projectID},
		ProcessKey: req.ProcessKey,
		Name:       req.Name,
		Scenario:   req.Scenario,
		Expect:     req.Expect,
	}
	if req.CaseID != "" {
		if c.ID, err = parseID("simulation case", req.CaseID); err != nil {
			return entities.SimulationCase{}, err
		}
	}
	return c, nil
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not a %s id", raw, what)
	}
	return id, nil
}
