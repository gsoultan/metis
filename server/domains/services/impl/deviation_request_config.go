package impl

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// EnvDeviationApprovalTTL names how long a request for a second administrator
// waits to be decided before it expires: a Go duration, such as 72h.
const EnvDeviationApprovalTTL = "METIS_DEVIATION_APPROVAL_TTL"

const (
	// DefaultDeviationApprovalTTL is how long a request waits when the
	// setting is not given, or cannot be read.
	DefaultDeviationApprovalTTL = 72 * time.Hour
	// MinDeviationApprovalTTL is the shortest a request may wait: less, and a
	// second administrator has no working hour to read it in.
	MinDeviationApprovalTTL = time.Hour
	// MaxDeviationApprovalTTL is the longest: a waive approved a month after
	// it was asked for is approved for an instance nobody remembers.
	MaxDeviationApprovalTTL = 720 * time.Hour
)

// DeviationApprovalTTL answers how long a request made now waits, and — when
// the setting was not usable as given — a sentence that says what was wrong
// and what is used instead. The sentence names the setting; it is empty when
// there is nothing to say.
//
// A value that cannot be read, or is not a length of time at all, falls back
// to the default; one outside the limits is brought to the nearer limit.
// Neither is silent: the sentence is logged when the server starts
// (logControlSettings), because a deadline nobody chose is still a deadline
// somebody's request expires at.
//
// Read on each request, as every METIS_* setting is: the environment does not
// change under a running process, and the deadline it gives is stored with
// the request, so a request keeps the deadline it was made with.
func DeviationApprovalTTL() (ttl time.Duration, problem string) {
	raw := envvar.Get(EnvDeviationApprovalTTL)
	if raw == "" {
		return DefaultDeviationApprovalTTL, ""
	}
	given, err := time.ParseDuration(raw)
	switch {
	case err != nil || given <= 0:
		return DefaultDeviationApprovalTTL, fmt.Sprintf("%s=%s cannot be read as a duration; requests wait %s",
			EnvDeviationApprovalTTL, raw, hoursOf(DefaultDeviationApprovalTTL))
	case given < MinDeviationApprovalTTL:
		return MinDeviationApprovalTTL, fmt.Sprintf("%s=%s is shorter than %s; requests wait %s",
			EnvDeviationApprovalTTL, raw, hoursOf(MinDeviationApprovalTTL), hoursOf(MinDeviationApprovalTTL))
	case given > MaxDeviationApprovalTTL:
		return MaxDeviationApprovalTTL, fmt.Sprintf("%s=%s is longer than %s; requests wait %s",
			EnvDeviationApprovalTTL, raw, hoursOf(MaxDeviationApprovalTTL), hoursOf(MaxDeviationApprovalTTL))
	}
	return given, ""
}

// hoursOf writes a whole number of hours as the setting is written: 72h.
func hoursOf(d time.Duration) string {
	return fmt.Sprintf("%dh", int64(d/time.Hour))
}

// EnvAllowSoleAdministratorSelfApproval names the one exception to the second
// administrator's approval: with it on, the administrator who asked for a
// request may approve it themselves — in an organization nobody else
// administers, and nowhere else.
//
// It reopens what the control closes: a waive made on one person's say. So
// it is off by default, and narrow when on. It applies only while no other
// account that is not deleted, belongs to the organization and holds the
// administrator role there — on the account, or in that organization alone —
// exists; the moment one does, the requester is refused as on any
// installation. Each use needs a reason, and is recorded as nobody else's
// approval: a deviation_self_approved entry on the trail, self_approved on
// the ledger row, and a line in the server's log.
//
// It is the installation's, not an organization's and not a request's: read
// from the environment when the server is put together
// (services.NewServiceFacade) and given to the service of requests as it is
// built (WithSoleAdministratorSelfApproval). Nothing reads it afterwards.
const EnvAllowSoleAdministratorSelfApproval = "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL"

// AllowSoleAdministratorSelfApproval answers whether the setting is on, and —
// when it was written so that it cannot be read — a sentence that says so.
// The sentence names the setting and what was written; it is empty when there
// is nothing to say.
//
// On is a value strconv.ParseBool reads as true: 1, t, T, true, TRUE, True.
// Not given, empty, or one it reads as false, is off and nothing is said.
// Anything else — "yes", "on", "true " with a space after it — is off too:
// an exception to a control is not switched on by a value that has to be
// guessed at. But whoever wrote it believes it is on, so that is said when
// the server starts (logControlSettings), where the two older settings of
// this kind read such a value as off without a word. What was written is
// quoted, so that a stray space shows, and cut at 64 characters.
//
// Read through envvar.Get, as every METIS_* setting is — so the spelling
// from before the rename is honoured too, with that package's warning.
func AllowSoleAdministratorSelfApproval() (allowed bool, problem string) {
	raw := envvar.Get(EnvAllowSoleAdministratorSelfApproval)
	if raw == "" {
		return false, ""
	}
	allowed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Sprintf("%s=%.64q cannot be read as true or false; it is off", EnvAllowSoleAdministratorSelfApproval, raw)
	}
	return allowed, ""
}
