package impl

import (
	"fmt"
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
