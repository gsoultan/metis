package participant

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// MakeListParticipantDevicesEndpoint lists one person's push devices.
func MakeListParticipantDevicesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ParticipantDevicesRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a ParticipantDevicesRequest, got %T", request)
		}
		projectID, id, err := parseProjectAndParticipant(req.ProjectID, req.ID)
		if err != nil {
			return ParticipantDevicesResponse{Err: err}, nil
		}
		devices, err := s.ListParticipantDevices(ctx, projectID, id)
		if devices == nil {
			devices = []entities.PersonDevice{}
		}
		return ParticipantDevicesResponse{Devices: devices, Err: err}, nil
	}
}

// MakeDeviceCountsEndpoint says how many devices each person in a project has.
func MakeDeviceCountsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeviceCountsRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a DeviceCountsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return DeviceCountsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier", req.ProjectID)}, nil
		}
		counts, err := s.CountParticipantDevices(ctx, projectID)
		if err != nil {
			return DeviceCountsResponse{Err: err}, nil
		}
		out := make(map[string]int, len(counts))
		for participantID, count := range counts {
			out[participantID.String()] = count
		}
		return DeviceCountsResponse{Counts: out}, nil
	}
}

// MakeRemoveParticipantDeviceEndpoint removes a person's device, such as a
// phone they lost.
func MakeRemoveParticipantDeviceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RemoveParticipantDeviceRequest)
		if !ok {
			return nil, fmt.Errorf("participant: expected a RemoveParticipantDeviceRequest, got %T", request)
		}
		projectID, id, err := parseProjectAndParticipant(req.ProjectID, req.ID)
		if err != nil {
			return RemoveParticipantDeviceResponse{Err: err}, nil
		}
		deviceID, err := uuid.Parse(req.DeviceID)
		if err != nil {
			return RemoveParticipantDeviceResponse{Err: apierr.Invalidf("device id %q is not a valid identifier", req.DeviceID)}, nil
		}
		return RemoveParticipantDeviceResponse{Err: s.RemoveParticipantDevice(ctx, projectID, id, deviceID)}, nil
	}
}

func parseProjectAndParticipant(project, participant string) (uuid.UUID, uuid.UUID, error) {
	projectID, err := uuid.Parse(project)
	if err != nil {
		return uuid.Nil, uuid.Nil, apierr.Invalidf("project_id %q is not a valid identifier", project)
	}
	id, err := uuid.Parse(participant)
	if err != nil {
		return uuid.Nil, uuid.Nil, apierr.Invalidf("id %q is not a valid identifier", participant)
	}
	return projectID, id, nil
}
