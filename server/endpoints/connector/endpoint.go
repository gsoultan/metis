package connector

import (
	"context"
	"errors"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	ListConnectors          endpoint.Endpoint
	CreateConnector         endpoint.Endpoint
	UpdateConnector         endpoint.Endpoint
	DeleteConnector         endpoint.Endpoint
	ListConnectorInstances  endpoint.Endpoint
	CreateConnectorInstance endpoint.Endpoint
	UpdateConnectorInstance endpoint.Endpoint
	DeleteConnectorInstance endpoint.Endpoint
	ExecuteConnector        endpoint.Endpoint
	TryConnectorStep        endpoint.Endpoint
	InstallManifest         endpoint.Endpoint
	ListManifests           endpoint.Endpoint
	GetManifest             endpoint.Endpoint
	SetManifestEnabled      endpoint.Endpoint
	DeleteManifest          endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListConnectors:          MakeListConnectorsEndpoint(s),
		CreateConnector:         MakeCreateConnectorEndpoint(s),
		UpdateConnector:         MakeUpdateConnectorEndpoint(s),
		DeleteConnector:         MakeDeleteConnectorEndpoint(s),
		ListConnectorInstances:  MakeListConnectorInstancesEndpoint(s),
		CreateConnectorInstance: MakeCreateConnectorInstanceEndpoint(s),
		UpdateConnectorInstance: MakeUpdateConnectorInstanceEndpoint(s),
		DeleteConnectorInstance: MakeDeleteConnectorInstanceEndpoint(s),
		ExecuteConnector:        MakeExecuteConnectorEndpoint(s),
		TryConnectorStep:        MakeTryConnectorStepEndpoint(s),
		InstallManifest:         MakeInstallManifestEndpoint(s),
		ListManifests:           MakeListManifestsEndpoint(s),
		GetManifest:             MakeGetManifestEndpoint(s),
		SetManifestEnabled:      MakeSetManifestEnabledEndpoint(s),
		DeleteManifest:          MakeDeleteManifestEndpoint(s),
	}
}

func MakeListConnectorsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		res, err := s.ListConnectors(ctx)
		return ListConnectorsResponse{Connectors: res, Err: err}, nil
	}
}

func MakeCreateConnectorEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateConnectorRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a CreateConnectorRequest, got %T", request)
		}
		res, err := s.CreateConnector(ctx, req.Connector)
		return CreateConnectorResponse{Connector: res, Err: err}, nil
	}
}

func MakeUpdateConnectorEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateConnectorRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a UpdateConnectorRequest, got %T", request)
		}
		err := s.UpdateConnector(ctx, req.Connector)
		return UpdateConnectorResponse{Err: err}, nil
	}
}

func MakeDeleteConnectorEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteConnectorRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a DeleteConnectorRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteConnectorResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.DeleteConnector(ctx, id)
		return DeleteConnectorResponse{Err: err}, nil
	}
}

func MakeListConnectorInstancesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListConnectorInstancesRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a ListConnectorInstancesRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListConnectorInstancesResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		res, err := s.ListConnectorInstances(ctx, projectID)
		return ListConnectorInstancesResponse{Instances: maskInstances(res), Err: err}, nil
	}
}

func MakeCreateConnectorInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateConnectorInstanceRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a CreateConnectorInstanceRequest, got %T", request)
		}
		res, err := s.CreateConnectorInstance(ctx, req.Instance)
		if err == nil {
			res.Config = configsecret.Mask(res.Config)
		}
		return CreateConnectorInstanceResponse{Instance: res, Err: err}, nil
	}
}

func MakeUpdateConnectorInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateConnectorInstanceRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a UpdateConnectorInstanceRequest, got %T", request)
		}
		// The form was shown placeholders in place of the stored credentials, so
		// fold them back onto what is held rather than saving the placeholder.
		// Without this, editing a connector's URL would overwrite its password
		// with the literal string the browser was shown.
		instance := req.Instance
		if instance.ID != uuid.Nil {
			if current, readErr := s.GetConnectorInstance(ctx, instance.ID); readErr == nil {
				instance.Config = configsecret.Merge(instance.Config, current.Config)
			} else if configsecret.HasSentinel(instance.Config) {
				// Refusing beats writing the placeholder as a credential.
				return UpdateConnectorInstanceResponse{
					Err: apierr.Invalidf("could not read the stored connection to keep its saved credentials: %v", readErr),
				}, nil
			}
		}
		err := s.UpdateConnectorInstance(ctx, instance)
		return UpdateConnectorInstanceResponse{Err: err}, nil
	}
}

func MakeDeleteConnectorInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteConnectorInstanceRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a DeleteConnectorInstanceRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteConnectorInstanceResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.DeleteConnectorInstance(ctx, id)
		return DeleteConnectorInstanceResponse{Err: err}, nil
	}
}

func MakeExecuteConnectorEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ExecuteConnectorRequest)
		if !ok {
			return nil, fmt.Errorf("connector: expected a ExecuteConnectorRequest, got %T", request)
		}
		res, err := s.ExecuteConnector(ctx, req.ConnectorKey, req.Config, req.Payload)
		return ExecuteConnectorResponse{Result: res, Err: err}, nil
	}
}

// MakeTryConnectorStepEndpoint runs one connector step against its project's
// saved connection, so a designer can see what the step would store.
func MakeTryConnectorStepEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(TryConnectorStepRequest)
		if !ok {
			return TryConnectorStepResponse{Err: errWrongConnectorRequest}, nil
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return TryConnectorStepResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier", req.ProjectID)}, nil
		}
		step := entities.Node{ID: "try", Name: req.StepName, Type: entities.ServiceTask, Properties: req.Properties}
		stored, err := s.TryConnectorStep(ctx, projectID, step, req.Variables)
		return TryConnectorStepResponse{Variables: stored, Err: err}, nil
	}
}

// errWrongConnectorRequest guards the type assertions below. It cannot happen
// through the handlers here, and asserting without checking would turn a future
// mis-wiring into a panic in a request goroutine.
var errWrongConnectorRequest = errors.New("connector: the request was not of the expected type")

// MakeInstallManifestEndpoint installs a connector written as a document.
//
// One endpoint for both formats rather than two: what a person has in front of
// them is "a file the vendor published", and being asked which of two upload
// buttons it belongs to is a question about our implementation.
func MakeInstallManifestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(InstallManifestRequest)
		if !ok {
			return InstallManifestResponse{Err: errWrongConnectorRequest}, nil
		}
		if req.Format == "openapi" {
			manifests, err := s.ImportOpenAPI(ctx, []byte(req.Document))
			return InstallManifestResponse{Manifests: manifests, Err: err}, nil
		}
		manifest, err := s.InstallManifest(ctx, []byte(req.Document))
		if err != nil {
			return InstallManifestResponse{Err: err}, nil
		}
		return InstallManifestResponse{Manifests: []entities.ConnectorManifest{manifest}}, nil
	}
}

func MakeListManifestsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		manifests, err := s.ListManifests(ctx)
		return ListManifestsResponse{Manifests: manifests, Err: err}, nil
	}
}

func MakeGetManifestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetManifestRequest)
		if !ok {
			return GetManifestResponse{Err: errWrongConnectorRequest}, nil
		}
		document, err := s.GetManifestDocument(ctx, req.Key)
		return GetManifestResponse{Document: document, Err: err}, nil
	}
}

func MakeSetManifestEnabledEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetManifestEnabledRequest)
		if !ok {
			return SetManifestEnabledResponse{Err: errWrongConnectorRequest}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return SetManifestEnabledResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		return SetManifestEnabledResponse{Err: s.SetManifestEnabled(ctx, id, req.Enabled)}, nil
	}
}

func MakeDeleteManifestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteManifestRequest)
		if !ok {
			return DeleteManifestResponse{Err: errWrongConnectorRequest}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteManifestResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		return DeleteManifestResponse{Err: s.DeleteManifest(ctx, id)}, nil
	}
}

// maskInstances replaces every stored credential with a placeholder.
//
// This is the API boundary, deliberately: the executor reads instances through
// the service directly and still needs the real values. Masking any deeper
// would break the calls the credentials exist for.
func maskInstances(instances []entities.ConnectorInstance) []entities.ConnectorInstance {
	for i := range instances {
		instances[i].Config = configsecret.Mask(instances[i].Config)
	}
	return instances
}
