package contracts

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRequestExpirer writes down what the clock has decided about the
// requests for a second administrator: those nobody decided before their
// deadline have expired, and approved ones whose run did not report back in
// the time a run is given were interrupted. It is the server's own work, run
// on a schedule, not something a request asks for.
type DeviationRequestExpirer interface {
	// SweepDeviationRequests closes every request the clock has closed at
	// now, and answers how many of each kind it closed. Beside an error it
	// still answers what it did close.
	SweepDeviationRequests(ctx context.Context, now time.Time) (entities.SweptRequests, error)
}
