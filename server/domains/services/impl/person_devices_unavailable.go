package impl

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

var errDevicesUnavailable = apierr.Invalidf("push devices need the People directory, which needs PostgreSQL")

// unavailablePersonDevices answers every device call on an installation with
// no People directory.
type unavailablePersonDevices struct{}

// NewUnavailablePersonDeviceService refuses every device call with the reason.
func NewUnavailablePersonDeviceService() *unavailablePersonDevices {
	return &unavailablePersonDevices{}
}

func (unavailablePersonDevices) RegisterPersonDevice(context.Context, entities.Person, entities.PersonDevice) (entities.PersonDevice, error) {
	return entities.PersonDevice{}, errDevicesUnavailable
}

func (unavailablePersonDevices) ListPersonDevices(context.Context, entities.Person) ([]entities.PersonDevice, error) {
	return nil, errDevicesUnavailable
}

func (unavailablePersonDevices) RemovePersonDevice(context.Context, entities.Person, uuid.UUID) error {
	return errDevicesUnavailable
}

func (unavailablePersonDevices) ListParticipantDevices(context.Context, uuid.UUID, uuid.UUID) ([]entities.PersonDevice, error) {
	return nil, errDevicesUnavailable
}

func (unavailablePersonDevices) CountParticipantDevices(context.Context, uuid.UUID) (map[uuid.UUID]int, error) {
	return nil, errDevicesUnavailable
}

func (unavailablePersonDevices) RemoveParticipantDevice(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errDevicesUnavailable
}
