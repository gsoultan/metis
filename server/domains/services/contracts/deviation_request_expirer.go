package contracts

import (
	"context"
	"time"
)

// DeviationRequestExpirer writes down what the clock has decided: requests
// nobody decided before their deadline. It is the server's own work, run on a
// schedule, not something a request asks for.
type DeviationRequestExpirer interface {
	// ExpireDeviationRequests closes every request that is overdue at now,
	// and answers how many it closed.
	ExpireDeviationRequests(ctx context.Context, now time.Time) (int64, error)
}
