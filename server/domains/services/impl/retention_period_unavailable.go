package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// errNoRetentionPeriods is what every method of a facade built without the
// retention period service answers, for the reason errNoRetentionHolds gives.
var errNoRetentionPeriods = errors.New("retention periods are unavailable: this server was wired without them")

// unavailableRetentionPeriods is the Null Object for a facade built without
// the retention period service.
type unavailableRetentionPeriods struct{}

// NewUnavailableRetentionPeriodService returns a service that refuses every
// call, for a facade built without the real one.
func NewUnavailableRetentionPeriodService() contracts.RetentionPeriodService {
	return unavailableRetentionPeriods{}
}

func (unavailableRetentionPeriods) SetRetentionPeriod(context.Context, uuid.UUID, string, *int, string) (entities.RetentionPeriod, error) {
	return entities.RetentionPeriod{}, errNoRetentionPeriods
}

func (unavailableRetentionPeriods) RetentionPeriods(context.Context, uuid.UUID) ([]entities.RetentionPeriod, error) {
	return nil, errNoRetentionPeriods
}

func (unavailableRetentionPeriods) RetentionPeriodHistory(context.Context, uuid.UUID, string) ([]entities.RetentionPeriod, error) {
	return nil, errNoRetentionPeriods
}
