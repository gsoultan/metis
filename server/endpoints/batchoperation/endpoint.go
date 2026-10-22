package batchoperation

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// Endpoints start, read and stop batch operations.
type Endpoints struct {
	StartBatchOperation endpoint.Endpoint
	GetBatchOperation   endpoint.Endpoint
	ListBatchOperations endpoint.Endpoint
	BatchOperationItems endpoint.Endpoint
	StopBatchOperation  endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		StartBatchOperation: makeStartEndpoint(s),
		GetBatchOperation:   makeGetEndpoint(s),
		ListBatchOperations: makeListEndpoint(s),
		BatchOperationItems: makeItemsEndpoint(s),
		StopBatchOperation:  makeStopEndpoint(s),
	}
}

func makeStartEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(StartRequest)
		if !ok {
			return nil, fmt.Errorf("batch operation: expected a StartRequest, got %T", request)
		}
		project, err := parseID("project", req.ProjectID)
		if err != nil {
			return StartResponse{Err: err}, nil
		}
		filter := entities.BatchOperationFilter{
			Status:         req.Filter.Status,
			BusinessKey:    req.Filter.BusinessKey,
			NeedsAttention: req.Filter.NeedsAttention,
		}
		if req.Filter.DefinitionID != "" {
			if filter.DefinitionID, err = parseID("definition", req.Filter.DefinitionID); err != nil {
				return StartResponse{Err: err}, nil
			}
		}
		outcome, err := s.StartBatchOperation(ctx, entities.BatchOperationRequest{
			ProjectID: project,
			Kind:      entities.BatchOperationKind(req.Kind),
			Filter:    filter,
			Reason:    req.Reason,
			// A dry run unless the caller says otherwise: acting on thousands of
			// instances is never what a request that forgot the field meant.
			DryRun: req.DryRun == nil || *req.DryRun,
		})
		if err != nil {
			return StartResponse{Err: err}, nil
		}
		reply := StartResponse{
			Kind:    string(outcome.Preview.Kind),
			Matched: outcome.Preview.Matched,
			TooMany: outcome.Preview.TooMany,
			Limit:   entities.MaxBatchOperationInstances,
		}
		if outcome.Operation != nil {
			view := ViewOf(*outcome.Operation)
			reply.Operation = &view
		}
		return reply, nil
	}
}

func makeGetEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(OperationRequest)
		if !ok {
			return nil, fmt.Errorf("batch operation: expected an OperationRequest, got %T", request)
		}
		id, err := parseID("batch operation", req.ID)
		if err != nil {
			return OperationResponse{Err: err}, nil
		}
		operation, err := s.BatchOperation(ctx, id)
		if err != nil {
			return OperationResponse{Err: err}, nil
		}
		view := ViewOf(operation)
		return OperationResponse{Operation: &view}, nil
	}
}

func makeListEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListRequest)
		if !ok {
			return nil, fmt.Errorf("batch operation: expected a ListRequest, got %T", request)
		}
		project, err := parseID("project", req.ProjectID)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		operations, err := s.ListBatchOperations(ctx, project)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		views := make([]OperationView, 0, len(operations))
		for _, operation := range operations {
			views = append(views, ViewOf(operation))
		}
		return ListResponse{Operations: views}, nil
	}
}

func makeItemsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ItemsRequest)
		if !ok {
			return nil, fmt.Errorf("batch operation: expected an ItemsRequest, got %T", request)
		}
		id, err := parseID("batch operation", req.ID)
		if err != nil {
			return ItemsResponse{Err: err}, nil
		}
		page, err := s.BatchOperationItems(ctx, id, entities.BatchItemStatus(req.Status),
			contracts.Pagination{Page: req.Page, PageSize: req.PageSize})
		if err != nil {
			return ItemsResponse{Err: err}, nil
		}
		return ItemsOf(page), nil
	}
}

func makeStopEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(StopRequest)
		if !ok {
			return nil, fmt.Errorf("batch operation: expected a StopRequest, got %T", request)
		}
		id, err := parseID("batch operation", req.ID)
		if err != nil {
			return OperationResponse{Err: err}, nil
		}
		operation, err := s.StopBatchOperation(ctx, id, req.Reason)
		if err != nil {
			return OperationResponse{Err: err}, nil
		}
		view := ViewOf(operation)
		return OperationResponse{Operation: &view}, nil
	}
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%s id %q is not a valid identifier", what, raw)
	}
	return id, nil
}
