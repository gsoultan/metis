package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PersonDeviceService lets a signed-in person, or their app holding one of
// their API keys, register the devices they want pushes on.
type PersonDeviceService interface {
	// RegisterPersonDevice records a device, or refreshes it when the token
	// is already known, and returns it.
	RegisterPersonDevice(ctx context.Context, person entities.Person, device entities.PersonDevice) (entities.PersonDevice, error)
	ListPersonDevices(ctx context.Context, person entities.Person) ([]entities.PersonDevice, error)
	RemovePersonDevice(ctx context.Context, person entities.Person, id uuid.UUID) error
}

// ParticipantDeviceService lets an administrator see and remove the devices
// of the people in their project, such as a phone somebody lost.
type ParticipantDeviceService interface {
	ListParticipantDevices(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.PersonDevice, error)
	// CountParticipantDevices says how many devices each person in the
	// project has; a person with none is absent.
	CountParticipantDevices(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]int, error)
	RemoveParticipantDevice(ctx context.Context, projectID, participantID, id uuid.UUID) error
}

// DeviceService is both halves, as the facade carries them.
type DeviceService interface {
	PersonDeviceService
	ParticipantDeviceService
}
