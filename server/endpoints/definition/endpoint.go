package definition

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/principal"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

type Endpoints struct {
	ListDefinitions           endpoint.Endpoint
	CreateDefinition          endpoint.Endpoint
	GetDefinition             endpoint.Endpoint
	DeleteDefinition          endpoint.Endpoint
	ExportDefinition          endpoint.Endpoint
	ImportDefinition          endpoint.Endpoint
	ListJavaScriptConditions  endpoint.Endpoint
	ListScriptTasks           endpoint.Endpoint
	PromoteDefinition         endpoint.Endpoint
	ListDefinitionVersions    endpoint.Endpoint
	ListLiveVersions          endpoint.Endpoint
	ScheduleDefinition        endpoint.Endpoint
	CancelScheduledDefinition endpoint.Endpoint
	MigrateInstances          endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListDefinitions:           MakeListDefinitionsEndpoint(s),
		CreateDefinition:          MakeCreateDefinitionEndpoint(s),
		GetDefinition:             MakeGetDefinitionEndpoint(s),
		DeleteDefinition:          MakeDeleteDefinitionEndpoint(s),
		ExportDefinition:          MakeExportDefinitionEndpoint(s),
		ImportDefinition:          MakeImportDefinitionEndpoint(s),
		ListJavaScriptConditions:  MakeListJavaScriptConditionsEndpoint(s),
		ListScriptTasks:           MakeListScriptTasksEndpoint(s),
		PromoteDefinition:         MakePromoteDefinitionEndpoint(s),
		ListDefinitionVersions:    MakeListDefinitionVersionsEndpoint(s),
		ListLiveVersions:          MakeListLiveVersionsEndpoint(s),
		ScheduleDefinition:        MakeScheduleDefinitionEndpoint(s),
		CancelScheduledDefinition: MakeCancelScheduledDefinitionEndpoint(s),
		MigrateInstances:          MakeMigrateInstancesEndpoint(s),
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
				return ListDefinitionsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
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
			return GetDefinitionResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
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
		if req.Definition == nil {
			return CreateDefinitionResponse{Err: apierr.Invalidf("a definition is required")}, nil
		}
		id, err := s.DeployDefinition(ctx, req.Definition, !req.Stage)
		if err != nil {
			return CreateDefinitionResponse{Err: err}, nil
		}
		// Version is read back off the definition rather than recomputed: the
		// allocator may have retried past a competing deploy, so the number the
		// caller ends up with is not always the one that was first proposed.
		return CreateDefinitionResponse{
			ID:      id,
			Version: req.Definition.Version,
			Live:    deployedLive(ctx, s, req),
		}, nil
	}
}

// deployedLive says whether the version a deploy made is the one new instances
// start on.
//
// A promoted version is. A staged one is not — unless the process had no live
// version, and then the only version is the one they get. The reply copied the
// request, so staging a process's first version answered "not live". Asked
// rather than assumed; if the answer cannot be read, the request's intent is
// the best guess left.
func deployedLive(ctx context.Context, s services.ServiceFacade, req CreateDefinitionRequest) bool {
	if !req.Stage {
		return true
	}
	var projectID uuid.UUID
	if req.Definition.Project != nil {
		projectID = req.Definition.Project.ID
	}
	live, err := s.ListLiveVersions(ctx, projectID)
	if err != nil {
		return false
	}
	version, chosen := live[req.Definition.Key]
	if !chosen {
		// Nobody has chosen, so the highest version is live, and this deploy
		// has just made the highest. Staging a later version pins the one
		// that was live first, so this is the first version.
		return true
	}
	return version == req.Definition.Version
}

// MakePromoteDefinitionEndpoint makes one deployed version the one new
// instances start on.
func MakePromoteDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(PromoteDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a PromoteDefinitionRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return PromoteDefinitionResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		if req.Key == "" {
			return PromoteDefinitionResponse{Err: apierr.Invalidf("a process key is required")}, nil
		}
		return PromoteDefinitionResponse{Err: s.PromoteDefinitionVersion(ctx, projectID, req.Key, req.Version)}, nil
	}
}

// MakeListDefinitionVersionsEndpoint returns one process key's version history
// with its live flag and instance counts.
func MakeListDefinitionVersionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDefinitionVersionsRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ListDefinitionVersionsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListDefinitionVersionsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		if req.Key == "" {
			return ListDefinitionVersionsResponse{Err: apierr.Invalidf("a process key is required")}, nil
		}
		versions, err := s.ListDefinitionVersions(ctx, projectID, req.Key)
		return ListDefinitionVersionsResponse{Versions: versions, Err: err}, nil
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
			return DeleteDefinitionResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
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
			return ExportDefinitionResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
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

// MakeListScriptTasksEndpoint serves the script-task inventory: every script
// task the caller's tenant can see.
//
// Nothing here is refused — this is not the javascript-conditions worklist. It
// is what sizes the sandbox's unbounded-memory gap, so that the fix is chosen
// from the scripts that exist rather than from a guess.
func MakeListScriptTasksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := request.(ListScriptTasksRequest); !ok {
			return nil, fmt.Errorf("definition: expected a ListScriptTasksRequest, got %T", request)
		}
		usages, err := s.ListScriptTasks(ctx)
		return ListScriptTasksResponse{Usages: usages, Err: err}, nil
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
			return ImportDefinitionResponse{Err: apierr.Invalidf("project_id must be a UUID: %v", err)}, nil
		}
		id, err := s.ImportDefinition(ctx, projectID, req.XML)
		return ImportDefinitionResponse{ID: id, Err: err}, nil
	}
}

// MakeListLiveVersionsEndpoint reports which version of each process key in a
// project new instances start on.
func MakeListLiveVersionsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListLiveVersionsRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ListLiveVersionsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListLiveVersionsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		live, err := s.ListLiveVersions(ctx, projectID)
		return ListLiveVersionsResponse{Live: live, Err: err}, nil
	}
}

// MakeScheduleDefinitionEndpoint arranges for a version to take over at a time.
func MakeScheduleDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ScheduleDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a ScheduleDefinitionRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ScheduleDefinitionResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		if req.Key == "" {
			return ScheduleDefinitionResponse{Err: apierr.Invalidf("a process key is required")}, nil
		}
		activateAt, err := time.Parse(time.RFC3339, req.ActivateAt)
		if err != nil {
			return ScheduleDefinitionResponse{Err: apierr.Invalidf("activate_at %q is not an RFC 3339 timestamp: %v", req.ActivateAt, err)}, nil
		}
		return ScheduleDefinitionResponse{Err: s.ScheduleDefinitionVersion(ctx, projectID, req.Key, req.Version, activateAt)}, nil
	}
}

// MakeCancelScheduledDefinitionEndpoint drops a cutover that has not happened.
func MakeCancelScheduledDefinitionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CancelScheduledDefinitionRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a CancelScheduledDefinitionRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return CancelScheduledDefinitionResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		releaseID, err := uuid.Parse(req.ReleaseID)
		if err != nil {
			return CancelScheduledDefinitionResponse{Err: apierr.Invalidf("release_id %q is not a valid identifier: %v", req.ReleaseID, err)}, nil
		}
		return CancelScheduledDefinitionResponse{Err: s.CancelScheduledVersion(ctx, projectID, releaseID)}, nil
	}
}

// MakeMigrateInstancesEndpoint moves running instances onto another version.
//
// Reachable for the first time here. It was written, hardened and left
// unreachable, which meant the one situation drain cannot cover — work in
// flight on a version that must not continue — had no answer in the product at
// all. It is administrative and it defaults to a dry run: this rewrites durable
// business commitments, so seeing the plan is the default and committing is the
// thing you ask for.
func MakeMigrateInstancesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(MigrateInstancesRequest)
		if !ok {
			return nil, fmt.Errorf("definition: expected a MigrateInstancesRequest, got %T", request)
		}
		source, err := uuid.Parse(req.SourceDefinitionID)
		if err != nil {
			return MigrateInstancesResponse{Err: apierr.Invalidf("source_definition_id %q is not a valid identifier: %v", req.SourceDefinitionID, err)}, nil
		}
		target, err := uuid.Parse(req.TargetDefinitionID)
		if err != nil {
			return MigrateInstancesResponse{Err: apierr.Invalidf("target_definition_id %q is not a valid identifier: %v", req.TargetDefinitionID, err)}, nil
		}

		// The actor is read even for a dry run, so a preview shows the same
		// refusals the apply would make rather than a friendlier set.
		selected := make([]uuid.UUID, 0, len(req.Instances))
		for _, raw := range req.Instances {
			id, parseErr := uuid.Parse(raw)
			if parseErr != nil {
				return MigrateInstancesResponse{Err: apierr.Invalidf("instances contains %q, which is not a valid identifier", raw)}, nil
			}
			selected = append(selected, id)
		}
		opts := []servicecontracts.MigrationOption{
			servicecontracts.WithAcknowledgedHolds(req.Acknowledge...),
			servicecontracts.WithNodeActions(req.NodeActions),
			servicecontracts.WithInstances(selected...),
		}
		if actor, actorErr := principal.Username(ctx); actorErr == nil {
			opts = append(opts, servicecontracts.WithActor(actor))
		}

		plan, err := s.PlanInstanceMigration(ctx, source, target, req.NodeMapping, opts...)
		if err != nil {
			return MigrateInstancesResponse{Err: err}, nil
		}
		if req.dryRun() {
			return MigrateInstancesResponse{Plan: plan}, nil
		}
		if err := s.MigrateInstances(ctx, source, target, req.NodeMapping, opts...); err != nil {
			// The plan comes back with the refusal so the caller sees both what
			// they asked for and why it was declined, in one reply.
			return MigrateInstancesResponse{Plan: plan, Err: err}, nil
		}
		return MigrateInstancesResponse{Plan: plan, Applied: true}, nil
	}
}
