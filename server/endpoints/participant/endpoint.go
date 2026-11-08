package participant

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/principal"
)

type Endpoints struct {
	ListParticipants        endpoint.Endpoint
	ListParticipantGroups   endpoint.Endpoint
	ImportParticipants      endpoint.Endpoint
	RemoveParticipant       endpoint.Endpoint
	SetParticipantPassword  endpoint.Endpoint
	ListDirectoryGaps       endpoint.Endpoint
	FillDirectoryGaps       endpoint.Endpoint
	ListStrandedTasks       endpoint.Endpoint
	ListHeldTasks           endpoint.Endpoint
	SetParticipantActive    endpoint.Endpoint
	ListParticipantDevices  endpoint.Endpoint
	DeviceCounts            endpoint.Endpoint
	RemoveParticipantDevice endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListParticipants:        MakeListParticipantsEndpoint(s),
		ListParticipantGroups:   MakeListParticipantGroupsEndpoint(s),
		ImportParticipants:      MakeImportParticipantsEndpoint(s),
		RemoveParticipant:       MakeRemoveParticipantEndpoint(s),
		SetParticipantPassword:  MakeSetParticipantPasswordEndpoint(s),
		ListDirectoryGaps:       MakeListDirectoryGapsEndpoint(s),
		FillDirectoryGaps:       MakeFillDirectoryGapsEndpoint(s),
		ListStrandedTasks:       MakeListStrandedTasksEndpoint(s),
		ListHeldTasks:           MakeListHeldTasksEndpoint(s),
		SetParticipantActive:    MakeSetParticipantActiveEndpoint(s),
		ListParticipantDevices:  MakeListParticipantDevicesEndpoint(s),
		DeviceCounts:            MakeDeviceCountsEndpoint(s),
		RemoveParticipantDevice: MakeRemoveParticipantDeviceEndpoint(s),
	}
}

func MakeListParticipantsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListParticipantsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ListParticipantsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListParticipantsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		people, err := s.ListWorkflowUsers(ctx, projectID, req.Limit)
		return ListParticipantsResponse{Participants: people, Err: err}, nil
	}
}

// MakeListParticipantGroupsEndpoint answers a project's People groups: the
// teams the designer offers a step's task to.
func MakeListParticipantGroupsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListParticipantGroupsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ListParticipantGroupsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListParticipantGroupsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		groups, err := s.ListWorkflowGroups(ctx, projectID)
		if groups == nil {
			groups = []entities.WorkflowGroup{}
		}
		return ListParticipantGroupsResponse{Groups: groups, Err: err}, nil
	}
}

// MakeImportParticipantsEndpoint reads a directory into a project.
//
// An import is additive: somebody absent from the source is left alone, not
// deactivated. A partial file is far more common than a complete one, and
// deactivating everybody it happened not to mention would take people's work
// away for a reason nobody could see. Removing somebody is a deliberate act.
func MakeImportParticipantsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ImportParticipantsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected an ImportParticipantsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ImportParticipantsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}

		var summary entities.ImportSummary
		switch req.Kind {
		case "csv", "":
			if len(req.CSV) == 0 {
				return ImportParticipantsResponse{Err: apierr.Invalidf("no file was uploaded")}, nil
			}
			summary, err = s.ImportWorkflowUsers(ctx, projectID, bytes.NewReader(req.CSV))
		case "http":
			summary, err = s.SyncWorkflowUsersFromHTTP(ctx, projectID, req.URL, req.Method)
		case "postgres":
			summary, err = s.SyncWorkflowUsersFromPostgres(ctx, projectID, req.DSN, req.Query)
		default:
			return ImportParticipantsResponse{Err: apierr.Invalidf("%q is not a directory source", req.Kind)}, nil
		}

		return ImportParticipantsResponse{
			Created:  summary.Created,
			Updated:  summary.Updated,
			Groups:   summary.Groups,
			Problems: summary.Problems,
			Err:      err,
		}, nil
	}
}

// MakeRemoveParticipantEndpoint takes somebody out of a project's directory.
//
// A removal is reversible: the row is marked rather than destroyed and keeps
// its key, so an import naming them again brings back the same person with
// their group memberships intact. That is what makes this safe to offer beside
// a bulk import — the two are the same act in opposite directions.
func MakeRemoveParticipantEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RemoveParticipantRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a RemoveParticipantRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return RemoveParticipantResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return RemoveParticipantResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		return RemoveParticipantResponse{Err: s.RemoveWorkflowUser(ctx, projectID, id)}, nil
	}
}

// MakeSetParticipantPasswordEndpoint gives a person a password for the People
// login. The project comes with the request and is checked against the
// person, as a removal's is.
func MakeSetParticipantPasswordEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetParticipantPasswordRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a SetParticipantPasswordRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SetParticipantPasswordResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return SetParticipantPasswordResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		return SetParticipantPasswordResponse{Err: s.SetWorkflowUserPassword(ctx, projectID, id, req.Password)}, nil
	}
}

// MakeListDirectoryGapsEndpoint answers the names a project's work uses that
// its People lack: the switch-over report.
func MakeListDirectoryGapsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDirectoryGapsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ListDirectoryGapsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListDirectoryGapsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		gaps, err := s.DirectoryGaps(ctx, projectID)
		if gaps == nil {
			gaps = []entities.DirectoryGap{}
		}
		return ListDirectoryGapsResponse{Gaps: gaps, Err: err}, nil
	}
}

// MakeFillDirectoryGapsEndpoint adds the named gaps to the project's People.
func MakeFillDirectoryGapsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(FillDirectoryGapsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a FillDirectoryGapsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return FillDirectoryGapsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		result, err := s.FillDirectoryGaps(ctx, projectID, req.People, req.Groups)
		return FillDirectoryGapsResponse{Result: result, Err: err}, nil
	}
}

// MakeListStrandedTasksEndpoint answers the project's open tasks nobody can
// act on.
func MakeListStrandedTasksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListStrandedTasksRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ListStrandedTasksRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListStrandedTasksResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		tasks, err := s.StrandedTasks(ctx, projectID)
		if tasks == nil {
			tasks = []entities.StrandedTask{}
		}
		return ListStrandedTasksResponse{Tasks: tasks, Err: err}, nil
	}
}

// MakeListHeldTasksEndpoint answers the open tasks one person holds.
func MakeListHeldTasksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListParticipantHeldTasksRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ListParticipantHeldTasksRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListParticipantHeldTasksResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return ListParticipantHeldTasksResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		tasks, err := s.HeldTasks(ctx, projectID, id)
		if tasks == nil {
			tasks = []entities.HeldTask{}
		}
		return ListParticipantHeldTasksResponse{Tasks: tasks, Err: err}, nil
	}
}

// MakeSetParticipantActiveEndpoint deactivates or reactivates a person. The
// tasks it moves are moved as hand-overs by the caller, named from the token.
func MakeSetParticipantActiveEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetParticipantActiveRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a SetParticipantActiveRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SetParticipantActiveResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return SetParticipantActiveResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		if req.Active == nil {
			return SetParticipantActiveResponse{Err: apierr.Invalidf("say whether they are to be active: {\"active\": true} or false")}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return SetParticipantActiveResponse{Err: err}, nil
		}
		result, err := s.SetParticipantActive(ctx, projectID, id, *req.Active, req.Work, actor)
		return SetParticipantActiveResponse{Result: result, Err: err}, nil
	}
}
