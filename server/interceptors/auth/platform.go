package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/interceptors/contracts"
	"github.com/rs/zerolog/log"
)

// PlatformAdministratorsEnv names the platform administrators: the account ids,
// comma-separated, of the administrators who may change what every
// organization on the installation runs.
//
// An operator setting rather than a role, because roles are granted through the
// API by administrators, and an administrator of any one organization is one.
// A power that must not be any one organization's cannot be granted by one;
// whoever runs the installation is the only party above them all. Account ids
// rather than usernames, because an administrator can rename an account in
// their organization, and could otherwise give one of theirs a listed name.
const PlatformAdministratorsEnv = "METIS_PLATFORM_ADMINS"

// platformAdministratorGate admits a caller to what every organization on the
// installation shares.
//
// An installation of one organization has nobody above its administrators, so
// it admits them, and upgrading one needs no configuration. On an installation
// of several it admits only the administrators the operator named.
type platformAdministratorGate struct {
	organizations  servicecontracts.OrganizationCounter
	administrators map[uuid.UUID]struct{}
}

// NewRequirePlatformAdministrator reads PlatformAdministratorsEnv once, here.
// It answers for the caller alone, so it belongs after authentication and after
// the administrator role check, whose refusal is the one a caller without the
// role should hear.
func NewRequirePlatformAdministrator(organizations servicecontracts.OrganizationCounter) contracts.EndpointInterceptor {
	return &platformAdministratorGate{
		organizations:  organizations,
		administrators: platformAdministrators(envvar.Get(PlatformAdministratorsEnv)),
	}
}

func (g *platformAdministratorGate) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		caller, local := accountID(ctx)
		if _, named := g.administrators[caller]; local && named {
			return next(ctx, request)
		}
		organizations, err := g.organizations.CountOrganizations(ctx)
		if err != nil {
			// Refused rather than admitted: not knowing whether the
			// installation is shared is not knowing whether this is allowed.
			return nil, fmt.Errorf("could not tell whether this installation has more than one organization: %w", err)
		}
		if organizations <= 1 {
			return next(ctx, request)
		}
		return nil, notAPlatformAdministrator(caller, local)
	}
}

// notAPlatformAdministrator names who can grant this and what to hand them.
func notAPlatformAdministrator(caller uuid.UUID, local bool) error {
	account := "a local account's id"
	if local {
		account = "your account id, " + caller.String() + ","
	}
	return apierr.Forbiddenf("this changes what every organization on this installation runs, so where there is more "+
		"than one organization it is for platform administrators only; whoever operates the installation makes you "+
		"one by adding %s to %s", account, PlatformAdministratorsEnv)
}

// accountID is the signed-in caller's local account id. An OIDC principal has
// none, and so cannot be named.
func accountID(ctx context.Context) (uuid.UUID, bool) {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		return u.ID, u.ID != uuid.Nil
	case *entities.User:
		if u != nil {
			return u.ID, u.ID != uuid.Nil
		}
	}
	return uuid.Nil, false
}

// platformAdministrators parses the operator's list. An entry that is not an
// account id is reported by position rather than echoed, since what was
// mistakenly put there may be somebody's name or email, and ignored: it names
// nobody.
func platformAdministrators(list string) map[uuid.UUID]struct{} {
	named := make(map[uuid.UUID]struct{})
	for position, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, err := uuid.Parse(entry)
		if err != nil || id == uuid.Nil {
			log.Warn().Str("setting", PlatformAdministratorsEnv).Int("entry", position+1).
				Msg("Ignoring an entry that is not an account id; platform administrators are named by account id")
			continue
		}
		named[id] = struct{}{}
	}
	return named
}
