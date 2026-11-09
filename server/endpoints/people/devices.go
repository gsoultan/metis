package people

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// MakeListDevicesEndpoint lists the signed-in person's push devices.
func MakeListDevicesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		person, err := signedInPerson(ctx)
		if err != nil {
			return DevicesResponse{Err: err}, nil
		}
		devices, err := s.ListPersonDevices(ctx, person)
		if devices == nil {
			devices = []entities.PersonDevice{}
		}
		return DevicesResponse{Devices: devices, Err: err}, nil
	}
}

// MakeRegisterDeviceEndpoint records a device for the signed-in person.
func MakeRegisterDeviceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RegisterDeviceRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a RegisterDeviceRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return RegisterDeviceResponse{Err: err}, nil
		}
		device, err := s.RegisterPersonDevice(ctx, person, entities.PersonDevice{
			Token: req.Token, Platform: req.Platform, Label: req.Label,
		})
		if err != nil {
			return RegisterDeviceResponse{Err: err}, nil
		}
		return RegisterDeviceResponse{Device: &device}, nil
	}
}

// MakeRemoveDeviceEndpoint removes one of the signed-in person's devices.
func MakeRemoveDeviceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RemoveDeviceRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a RemoveDeviceRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return PersonTaskActionResponse{Err: apierr.Invalidf("device id %q is not a valid identifier", req.ID)}, nil
		}
		return PersonTaskActionResponse{Err: s.RemovePersonDevice(ctx, person, id)}, nil
	}
}
