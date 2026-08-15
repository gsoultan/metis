package decision

import (
	"context"

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
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListDecisions:    MakeListDecisionsEndpoint(s),
		GetDecision:      MakeGetDecisionEndpoint(s),
		CreateDecision:   MakeCreateDecisionEndpoint(s),
		DeleteDecision:   MakeDeleteDecisionEndpoint(s),
		UpdateDecision:   MakeUpdateDecisionEndpoint(s),
		EvaluateDecision: MakeEvaluateDecisionEndpoint(s),
	}
}

func MakeListDecisionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(ListDecisionsRequest)
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
		req := request.(GetDecisionRequest)
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
		req := request.(CreateDecisionRequest)
		id, err := s.CreateDecision(ctx, req.Decision)
		return CreateDecisionResponse{ID: id, Err: err}, nil
	}
}

func MakeDeleteDecisionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(DeleteDecisionRequest)
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
		req := request.(UpdateDecisionRequest)
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
		req := request.(EvaluateDecisionRequest)
		res, err := s.Evaluate(ctx, req.Key, req.Version, req.Variables)
		return EvaluateDecisionResponse{Result: res, Err: err}, nil
	}
}
