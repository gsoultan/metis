package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

type deviationLedger struct {
	repo repocontracts.DeviationRepository
}

// NewDeviationLedger builds the ledger over the repository. A nil repository,
// or one with no deviation store, yields a ledger that refuses every write and
// read, so a change that must be recorded is not made without it.
func NewDeviationLedger(repo repositories.Repository) contracts.DeviationLedger {
	if repo == nil || repo.Deviation() == nil {
		return unavailableLedger{}
	}
	return &deviationLedger{repo: repo.Deviation()}
}

func (l *deviationLedger) Record(ctx context.Context, deviation entities.Deviation) (entities.Deviation, error) {
	prepared, err := prepareDeviation(ctx, deviation)
	if err != nil {
		return entities.Deviation{}, err
	}
	return l.repo.Create(ctx, prepared)
}

func (l *deviationLedger) ListInstanceDeviations(ctx context.Context, instanceID uuid.UUID) ([]entities.Deviation, error) {
	return l.repo.ListByInstance(ctx, instanceID)
}
