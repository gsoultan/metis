package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// errNoRetentionHolds is what every method of a facade built without the
// retention hold service answers: a plain error, because it is how the server
// was put together. Answering "not held" would tell an administrator an
// instance can be swept when nobody could have held it.
var errNoRetentionHolds = errors.New("retention holds are unavailable: this server was wired without them")

// unavailableRetentionHolds is the Null Object for a facade built without
// the retention hold service.
type unavailableRetentionHolds struct{}

// NewUnavailableRetentionHoldService returns a service that refuses every
// call, for a facade built without the real one.
func NewUnavailableRetentionHoldService() contracts.RetentionHoldService {
	return unavailableRetentionHolds{}
}

func (unavailableRetentionHolds) HoldInstance(context.Context, uuid.UUID, string) (entities.RetentionHold, error) {
	return entities.RetentionHold{}, errNoRetentionHolds
}

func (unavailableRetentionHolds) ReleaseInstanceHold(context.Context, uuid.UUID, string) (entities.RetentionHold, error) {
	return entities.RetentionHold{}, errNoRetentionHolds
}

func (unavailableRetentionHolds) InstanceHold(context.Context, uuid.UUID) (entities.RetentionHold, bool, error) {
	return entities.RetentionHold{}, false, errNoRetentionHolds
}

func (unavailableRetentionHolds) ListRetentionHolds(context.Context, uuid.UUID) ([]entities.RetentionHold, error) {
	return nil, errNoRetentionHolds
}
