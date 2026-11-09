package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PersonDeviceRepository keeps the phones and browsers People registered for
// push, in the main database beside the directory they belong to.
type PersonDeviceRepository interface {
	PersonOwnDevices
	ProjectDevices
}

// PersonOwnDevices is a signed-in person's view of their own devices. The
// participant comes from their verified sign-in, never from the request.
type PersonOwnDevices interface {
	// Register records a device for the participant, or moves it to them if
	// another person held the same token, and returns it as stored.
	Register(ctx context.Context, device entities.PersonDevice, tokenHash string) (entities.PersonDevice, error)
	// ListByParticipant returns a person's devices, newest first.
	ListByParticipant(ctx context.Context, participantID uuid.UUID) ([]entities.PersonDevice, error)
	// Remove deletes one of the person's devices. apierr.ErrNotFound when
	// they have no device with that id.
	Remove(ctx context.Context, participantID, id uuid.UUID) error
}

// ProjectDevices is an administrator's view of the devices in one project.
// Every method refuses a project outside the caller's tenant.
type ProjectDevices interface {
	// ListInProject returns one participant's devices, newest first.
	ListInProject(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.PersonDevice, error)
	// CountByProject says how many devices each participant in the project has.
	CountByProject(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]int, error)
	// RemoveInProject deletes one participant's device. apierr.ErrNotFound
	// when the project has no such device for them.
	RemoveInProject(ctx context.Context, projectID, participantID, id uuid.UUID) error
}
