// Package platformadmins decides who may change what every organization on an
// installation shares.
//
// On an installation of one organization that is its administrators: there is
// nobody above them, and upgrading one needs no configuration. On an
// installation of several it is the administrators the operator names in Env,
// because roles are granted through the API by administrators, and a power
// that must not be any one organization's cannot be granted by one.
//
// One decision, asked in two places: the endpoint gate on the connectors every
// organization runs, and the account service, which asks it before a role that
// acts in every organization is granted or taken away.
package platformadmins

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/rs/zerolog/log"
)

// Env names the platform administrators: the account ids, comma-separated, of
// the administrators who may change what every organization on the
// installation shares. Account ids rather than usernames, because an
// administrator can rename an account in their organization, and could
// otherwise give one of theirs a listed name.
const Env = "METIS_PLATFORM_ADMINS"

// OrganizationCounter says how many organizations share the installation,
// whoever asks.
type OrganizationCounter interface {
	CountOrganizations(ctx context.Context) (int64, error)
}

// Gate is the operator's list, read once, and the installation it is for.
type Gate struct {
	organizations OrganizationCounter
	named         map[uuid.UUID]struct{}
}

// New reads Env once, here, so a mistyped entry is reported once per gate
// rather than once per request.
func New(organizations OrganizationCounter) *Gate {
	return &Gate{organizations: organizations, named: Parse(envvar.Get(Env))}
}

// Admits reports whether the account with this id may change what every
// organization shares. uuid.Nil is somebody with no account here, and can be
// admitted only by an installation of one organization.
//
// Who holds which role is not its question: the callers ask for the
// administrator role, held in every organization, before they ask this.
//
// An error means the installation could not say how many organizations it has.
// Not knowing whether it is shared is not knowing whether this is allowed, so
// every caller refuses on one.
func (g *Gate) Admits(ctx context.Context, account uuid.UUID) (bool, error) {
	if _, named := g.named[account]; named && account != uuid.Nil {
		return true, nil
	}
	organizations, err := g.organizations.CountOrganizations(ctx)
	if err != nil {
		return false, fmt.Errorf("could not tell whether this installation has more than one organization: %w", err)
	}
	return organizations <= 1, nil
}

// Parse reads the operator's list. An entry that is not an account id is
// reported by position rather than echoed, since what was mistakenly put there
// may be somebody's name or email, and ignored: it names nobody. The nil id is
// ignored too — it would otherwise name everybody with no account here.
func Parse(list string) map[uuid.UUID]struct{} {
	named := make(map[uuid.UUID]struct{})
	for position, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, err := uuid.Parse(entry)
		if err != nil || id == uuid.Nil {
			log.Warn().Str("setting", Env).Int("entry", position+1).
				Msg("Ignoring an entry that is not an account id; platform administrators are named by account id")
			continue
		}
		named[id] = struct{}{}
	}
	return named
}
