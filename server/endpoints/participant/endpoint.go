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
)

type Endpoints struct {
	ListParticipants   endpoint.Endpoint
	ImportParticipants endpoint.Endpoint
	RemoveParticipant  endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListParticipants:   MakeListParticipantsEndpoint(s),
		ImportParticipants: MakeImportParticipantsEndpoint(s),
		RemoveParticipant:  MakeRemoveParticipantEndpoint(s),
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
