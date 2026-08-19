package decision

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/services"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
)

type Endpoints struct {
	ListDecisions    endpoint.Endpoint
	GetDecision      endpoint.Endpoint
	CreateDecision   endpoint.Endpoint
	DeleteDecision   endpoint.Endpoint
	UpdateDecision   endpoint.Endpoint
	EvaluateDecision endpoint.Endpoint
	DecisionImpact   endpoint.Endpoint
	RunTests         endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListDecisions:    MakeListDecisionsEndpoint(s),
		GetDecision:      MakeGetDecisionEndpoint(s),
		CreateDecision:   MakeCreateDecisionEndpoint(s),
		DeleteDecision:   MakeDeleteDecisionEndpoint(s),
		UpdateDecision:   MakeUpdateDecisionEndpoint(s),
		EvaluateDecision: MakeEvaluateDecisionEndpoint(s),
		DecisionImpact:   MakeDecisionImpactEndpoint(s),
		RunTests:         MakeRunDecisionTestsEndpoint(s),
	}
}

func MakeListDecisionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDecisionsRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a ListDecisionsRequest, got %T", request)
		}
		var projectID uuid.UUID
		var err error
		if req.ProjectID != "" {
			projectID, err = uuid.Parse(req.ProjectID)
			if err != nil {
				return ListDecisionsResponse{Err: err}, nil
			}
		}
		page, err := s.ListDecisionsPaged(ctx, projectID, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if err != nil {
			return ListDecisionsResponse{Err: err}, nil
		}
		return ListDecisionsResponse{
			Decisions: page.Items,
			Page: &PageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

func MakeGetDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetDecisionRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a GetDecisionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetDecisionResponse{Err: err}, nil
		}
		dec, err := s.GetDecision(ctx, id)
		return GetDecisionResponse{Decision: dec, Err: err}, nil
	}
}

func MakeCreateDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateDecisionRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a CreateDecisionRequest, got %T", request)
		}
		id, err := s.CreateDecision(ctx, req.Decision)
		return CreateDecisionResponse{ID: id, Err: err}, nil
	}
}

func MakeDeleteDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteDecisionRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a DeleteDecisionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteDecisionResponse{Err: err}, nil
		}
		err = s.DeleteDecision(ctx, id)
		return DeleteDecisionResponse{Err: err}, nil
	}
}

func MakeUpdateDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateDecisionRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a UpdateDecisionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return UpdateDecisionResponse{Err: err}, nil
		}
		err = s.UpdateDecision(ctx, id, req.Decision)
		return UpdateDecisionResponse{Err: err}, nil
	}
}

func MakeEvaluateDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(EvaluateDecisionRequest)
		if !ok {
			return nil, fmt.Errorf("decision: expected a EvaluateDecisionRequest, got %T", request)
		}
		res, err := s.Evaluate(ctx, req.Key, req.Version, req.Variables)
		return EvaluateDecisionResponse{Result: res, Err: err}, nil
	}
}

func MakeDecisionImpactEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DecisionImpactRequest)
		if !ok {
			return DecisionImpactResponse{Err: errWrongDecisionRequest}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DecisionImpactResponse{Err: err}, nil
		}
		impact, err := s.DecisionImpact(ctx, id)
		return DecisionImpactResponse{Impact: impact, Err: err}, nil
	}
}

// errWrongDecisionRequest guards the type assertion above. It cannot happen
// through the handlers here, and asserting without checking would turn a future
// mis-wiring into a panic in a request goroutine.
var errWrongDecisionRequest = errors.New("decision: the request was not of the expected type")

func MakeRunDecisionTestsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RunDecisionTestsRequest)
		if !ok {
			return RunDecisionTestsResponse{Err: errWrongDecisionRequest}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return RunDecisionTestsResponse{Err: err}, nil
		}
		results, err := s.RunDecisionTests(ctx, id)
		return RunDecisionTestsResponse{Results: results, Err: err}, nil
	}
}
