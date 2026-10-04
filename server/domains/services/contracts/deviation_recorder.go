package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRecorder writes the deviation ledger: what was done to an instance
// that its process did not decide.
type DeviationRecorder interface {
	// Record writes one deviation and returns it as stored, with the id and run
	// id it was given. ctx must carry the transaction that makes the change the
	// row records; outside one it refuses. Its error is the change's: a caller
	// that cannot record a deviation must not make the change.
	Record(ctx context.Context, deviation entities.Deviation) (entities.Deviation, error)
}
