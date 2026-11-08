package impl

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// unavailablePushSender is push on an installation with no People directory:
// never configured, and nobody to reach.
type unavailablePushSender struct{}

// NewUnavailablePushSender answers that push is not set up, rather than failing.
func NewUnavailablePushSender() *unavailablePushSender { return &unavailablePushSender{} }

func (unavailablePushSender) PushConfigured(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}

func (unavailablePushSender) PushToPerson(context.Context, uuid.UUID, uuid.UUID, entities.PushContent) (entities.PushResult, error) {
	return entities.PushResult{}, nil
}
