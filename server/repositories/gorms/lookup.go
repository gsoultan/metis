package gorms

import (
	"errors"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"gorm.io/gorm"
)

// lookupError turns a repository read into an error the transport can classify.
//
// GORM answers a miss with ErrRecordNotFound, which the HTTP encoder does not
// recognise — and anything it does not recognise becomes a 500. So asking for a
// task that has been completed and cleaned up, or following a bookmark to a
// deleted instance, was reported as a server failure: it spent the roadmap's
// 0.1% 5xx error budget and would page whoever is on call for a request the
// server had answered correctly.
//
// Measured before this existed: six of nine read endpoints returned 500 for a
// well-formed identifier that simply named nothing.
//
// This is the repository's job rather than the transport's. The repository is
// the layer that knows GORM; teaching the HTTP encoder about an ORM sentinel
// would put the database's vocabulary in the one place that should not have it.
//
// It is also the same shape as the fix for malformed identifiers, which turned
// a caller's typo from a 500 into a 400. Absent and malformed are both the
// caller's business, and neither is the server failing.
func lookupError(err error, subject string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Both sentinels are kept. Adding apierr.ErrNotFound is what gets the
		// status right; keeping GORM's is what stops this quietly changing an
		// answer somewhere else — the tenant scope denies a foreign row by
		// letting the read miss, and the isolation tests assert that shape.
		// Replacing the chain rather than extending it would have made a
		// status-code fix look like it needed a security test rewritten, which
		// is a trade nobody should be offered.
		return fmt.Errorf("%w: no such %s (%w)", apierr.ErrNotFound, subject, err)
	}
	return fmt.Errorf("could not get %s: %w", subject, err)
}
