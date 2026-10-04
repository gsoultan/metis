package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

var errNoDeviationLedger = errors.New("this server has no deviation ledger, so a change that must be recorded in one was not made")

// unavailableLedger is the ledger of a wiring that has none. It refuses every
// write and every read: a read that answered "nothing" would pass for an
// instance with a clean history.
type unavailableLedger struct{}

func (unavailableLedger) Record(context.Context, entities.Deviation) (entities.Deviation, error) {
	return entities.Deviation{}, errNoDeviationLedger
}

func (unavailableLedger) ListInstanceDeviations(context.Context, uuid.UUID) ([]entities.Deviation, error) {
	return nil, errNoDeviationLedger
}
