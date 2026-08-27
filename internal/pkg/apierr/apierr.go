// Package apierr carries the small set of failure kinds a transport must be
// able to tell apart.
//
// Without it every error reaching the HTTP encoder became a 500, including a
// caller sending a malformed identifier. That is not a cosmetic
// misclassification. The roadmap's §1 reliability target is a 0.1% 5xx error
// budget, and alerting hangs off the same signal — so a client typo spent the
// budget the engine's own failures are measured against, and could page
// somebody at night for a request that was answered exactly as it should have
// been.
//
// Kept deliberately small: these are the distinctions a transport acts on, not
// a taxonomy of everything that can go wrong.
package apierr

import (
	"errors"
	"fmt"
)

// ErrInvalidArgument marks a failure caused by what the caller sent — an
// unparseable identifier, a field that cannot mean anything. The request was
// understood well enough to be judged, and it was wrong: 400, not 500.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrNotFound marks a request for something that is not there, or that the
// caller may not see. Tenant scoping deliberately answers "not found" rather
// than "forbidden" for another organization's row, because the alternative
// confirms the row exists.
var ErrNotFound = errors.New("not found")

// Invalidf builds an ErrInvalidArgument with a message a caller can act on.
//
// The message should say which field and why, because "invalid argument" alone
// leaves the caller guessing at which of their fields the server disliked.
func Invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}
