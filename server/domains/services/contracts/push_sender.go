package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PushSender sends a push to every device a person registered, through their
// project's push connection.
type PushSender interface {
	// PushConfigured says whether the project has a Firebase connection
	// marked for push, so a settings page can say "not set up yet".
	PushConfigured(ctx context.Context, projectID uuid.UUID) (bool, error)
	// PushToPerson sends content to the participant's devices. Devices
	// Firebase says are gone are removed. An error means a fault worth trying
	// again later (Firebase unreachable, the connection refused); nobody to
	// reach is a result with Delivered == 0, not an error.
	PushToPerson(ctx context.Context, projectID, participantID uuid.UUID, content entities.PushContent) (entities.PushResult, error)
}
