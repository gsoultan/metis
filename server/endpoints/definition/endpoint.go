package definition

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/services"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
)

type Endpoints struct {
	ListDefinitions          endpoint.Endpoint
	CreateDefinition         endpoint.Endpoint
	GetDefinition            endpoint.Endpoint
	DeleteDefinition         endpoint.Endpoint
	ExportDefinition         endpoint.Endpoint
	ImportDefinition         endpoint.Endpoint
	ListJavaScriptConditions endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListDefinitions:          MakeListDefinitionsEndpoint(s),
		CreateDefinition:         MakeCreateDefinitionEndpoint(s),
		GetDefinition:            MakeGetDefinitionEndpoint(s),
		DeleteDefinition:         MakeDeleteDefinitionEndpoint(s),
		ExportDefinition:         MakeExportDefinitionEndpoint(s),
		ImportDefinition:         MakeImportDefinitionEndpoint(s),
		ListJavaScriptConditions: MakeListJavaScriptConditionsEndpoint(s),
	}
}

func MakeListDefinitionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDefinitionsRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ListDefinitionsRequest, got %T", request)
		}
		var projectID uuid.UUID
		var err error
		if req.ProjectID != "" {
			projectID, err = uuid.Parse(req.ProjectID)
			if err != nil {
				return ListDefinitionsResponse{Err: err}, nil
			}
		}
		// A project keeps every version of every process it has ever had, so
		// this is paged. A caller that asks for nothing gets the first page at
		// the server default rather than all of them.
		page, err := s.ListDefinitionsPaged(ctx, projectID, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if err != nil {
			return ListDefinitionsResponse{Err: err}, nil
		}
		return ListDefinitionsResponse{
			Definitions: page.Items,
			Page: &PageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

func MakeGetDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a GetDefinitionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetDefinitionResponse{Err: err}, nil
		}
		def, err := s.GetDefinition(ctx, id)
		return GetDefinitionResponse{Definition: def, Err: err}, nil
	}
}

func MakeCreateDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a CreateDefinitionRequest, got %T", request)
		}
		id, err := s.CreateDefinition(ctx, req.Definition)
		return CreateDefinitionResponse{ID: id, Err: err}, nil
	}
}

func MakeDeleteDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a DeleteDefinitionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteDefinitionResponse{Err: err}, nil
		}
		err = s.DeleteDefinition(ctx, id)
		return DeleteDefinitionResponse{Err: err}, nil
	}
}

func MakeExportDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ExportDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ExportDefinitionRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return ExportDefinitionResponse{Err: err}, nil
		}
		xml, err := s.ExportDefinition(ctx, id)
		return ExportDefinitionResponse{XML: xml, Err: err}, nil
	}
}

// MakeListJavaScriptConditionsEndpoint serves the javascript-conditions
// worklist: every stored `js:` condition the caller's tenant can see. The flag
// ships off, so this list is what stands between an installation and turning
// the flag's refusals into rewritten FEEL.
func MakeListJavaScriptConditionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := request.(ListJavaScriptConditionsRequest); !ok {
			return nil, fmt.Errorf("definition: expected a ListJavaScriptConditionsRequest, got %T", request)
		}
		usages, err := s.ListJavaScriptConditions(ctx)
		return ListJavaScriptConditionsResponse{Usages: usages, Err: err}, nil
	}
}

func MakeImportDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ImportDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ImportDefinitionRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ImportDefinitionResponse{Err: fmt.Errorf("project_id must be a UUID: %w", err)}, nil
		}
		id, err := s.ImportDefinition(ctx, projectID, req.XML)
		return ImportDefinitionResponse{ID: id, Err: err}, nil
	}
}
