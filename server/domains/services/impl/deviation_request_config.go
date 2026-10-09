package impl

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

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

// EnvSoleAdministratorOrganizations names the organizations where the one
// exception to the second administrator's approval applies: the ids,
// comma-separated, of the organizations whose only administrator may approve
// a request they asked for themselves.
//
// It reopens what the control closes — a waive made on one person's say — so
// it is narrow. Unset or empty, it applies nowhere. It applies only in an
// organization named here, and there only while no other account that is not
// deleted, belongs to the organization and holds the administrator role
// there — on the account, or in that organization alone — exists; the moment
// one does, the requester is refused as anywhere else. Each use needs a
// reason, and is recorded as nobody else's approval: a
// deviation_self_approved entry on the trail; self_approved, the
// organization and other_administrators on it and on the ledger row; and a
// line in the server's log.
//
// Organizations are named, rather than the exception switched on for the
// installation, because in an organization with two administrators either
// can take the other's role away, and would then be "the only one". Naming
// confines the exception to the organizations an operator has said have one
// administrator. It does not close that door in a named organization: an
// administrator there who can change roles can still make themselves the
// only one, approve their own request and give the role back. Each of those
// changes of roles is in the server's log with who made it
// (traceAccountChange), and on no trail. An organization belongs on the list
// only while it truly has one administrator.
//
// By id, as METIS_PLATFORM_ADMINS names accounts by id: an organization's
// name is neither unique nor permanent.
//
// It is the operator's, not an organization's and not a request's: read from
// the environment once, when the server starts
// (internal/app.readControlSettings), and given to the service of requests
// as it is built (WithSoleAdministratorOrganizations). Nothing reads it
// afterwards.
const EnvSoleAdministratorOrganizations = "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS"

// SoleAdministratorOrganizations answers the organizations the setting
// names, each once, in the order written — and a sentence for each entry
// that names none.
//
// Entries are separated by commas and the spaces around one are not part of
// it; an empty entry is nothing. An entry that is not an id names no
// organization and is ignored, and the entries beside it still apply. The
// nil id is ignored the same way: it is no organization, and would otherwise
// name every request that is for none. Such an entry is not silent: whoever
// wrote it believes it names somebody, so each gets a sentence that says
// which entry it was and quotes it, cut at 64 characters, for the server to
// say when it starts (logControlSettings). "true" is such an entry: nothing
// switches the exception on for every organization.
//
// It reads and says nothing: it logs nothing and keeps nothing, and answers
// the same for the same environment. The server calls it once, when it
// starts, and both what it announces and what it builds its services with
// are that one answer (internal/app.readControlSettings).
func SoleAdministratorOrganizations() (named []uuid.UUID, problems []string) {
	// os.LookupEnv, never envvar.Get: that package also answers to the
	// spelling from before the rename (GOBPM_…), and a setting that weakens
	// a control has one name — as METIS_ALLOW_WEAK_SECRETS has
	// (secrets.Allowed).
	written, _ := os.LookupEnv(EnvSoleAdministratorOrganizations)
	seen := map[uuid.UUID]struct{}{}
	for position, entry := range strings.Split(written, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, err := uuid.Parse(entry)
		if err != nil || id == uuid.Nil {
			problems = append(problems, fmt.Sprintf("entry %d of %s, %.64q, is not an organization id, so it names no organization and is ignored",
				position+1, EnvSoleAdministratorOrganizations, entry))
			continue
		}
		if _, already := seen[id]; already {
			continue
		}
		seen[id] = struct{}{}
		named = append(named, id)
	}
	return named, problems
}
