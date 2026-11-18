package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/identity"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
	"github.com/rs/zerolog/log"
)

// OwnCredentialPaths are the routes a person uses on their own credentials.
// With an Identity, the program in front of the engine serves them and the
// engine answers 404, so a client that reaches the engine directly is not
// signed in by a path that no longer checks anything.
var OwnCredentialPaths = map[string]bool{
	"/api/v1/login":                              true,
	"/api/v1/login/code":                         true,
	"/api/v1/login/enrol":                        true,
	"/api/v1/login/enrol/confirm":                true,
	"/api/v1/logout":                             true,
	"/api/v1/users/me/password":                  true,
	"/api/v1/users/me/two-factor":                true,
	"/api/v1/users/me/two-factor/recovery-codes": true,
}

// sessionOnlyPrefixes are the routes a key never reaches, whatever roles its
// organization gave it: who has an account and what it may do, the keys
// themselves, organizations, and the installation. A key that leaked would
// otherwise be a way to make more keys, or accounts, that outlive it.
var sessionOnlyPrefixes = []string{
	"/api/v1/users",
	"/api/v1/integration-keys",
	"/api/v1/platform-users",
	"/api/v1/organizations",
	"/api/v1/groups",
	"/api/v1/roles",
	"/api/v1/settings",
	"/api/v1/upgrades",
}

// SessionOnly reports whether a key is refused at path.
func SessionOnly(path string) bool {
	for _, prefix := range sessionOnlyPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

type identityHTTPAuthInterceptor struct {
	identity identity.Identity
	account  func(ctx context.Context, userID uuid.UUID) (entities.User, error)
	// people admits the People routes exactly as without an Identity: People
	// are a project's directory, not accounts, and stay the engine's.
	people        *mandatoryHTTPAuthInterceptor
	publicPathSet map[string]struct{}
}

// NewIdentityHTTPAuthInterceptor admits a request to /api/ as the Principal
// the embedding program's Identity proves, and as the engine's account of that
// id, read fresh: the engine decides what it may do from its own roles.
func NewIdentityHTTPAuthInterceptor(
	id identity.Identity,
	account func(ctx context.Context, userID uuid.UUID) (entities.User, error),
	people SecurityStrategy,
	publicPaths []string,
) contracts.TransportInterceptor {
	set := make(map[string]struct{}, len(publicPaths))
	for _, p := range publicPaths {
		set[p] = struct{}{}
	}
	inner := &mandatoryHTTPAuthInterceptor{publicPathSet: set, people: people}
	return &identityHTTPAuthInterceptor{identity: id, account: account, people: inner, publicPathSet: set}
}

func (i *identityHTTPAuthInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, PeoplePathPrefix) {
			i.people.servePerson(w, r, next)
			return
		}
		if OwnCredentialPaths[path] {
			writeJSONError(w, http.StatusNotFound, "signing in is served by the program in front of this engine")
			return
		}
		if i.people.isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		principal, ok, err := i.identity.Authenticate(r.Context(), r)
		if err != nil {
			// Never anonymous because the identity store failed: the caller
			// is told to try again, not that they are nobody.
			log.Warn().Err(err).Str("path", path).Msg("The identity could not authenticate a request")
			writeJSONError(w, http.StatusServiceUnavailable, "sign-in could not be checked; try again in a moment")
			return
		}
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Before anything is read for it: a key is refused here whoever it is.
		if principal.ViaKey && SessionOnly(path) {
			writeJSONError(w, http.StatusForbidden, "an integration key cannot manage accounts, keys, organizations or the installation; sign in as a person")
			return
		}
		user, admitted := i.principalUser(r.Context(), principal)
		if !admitted {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), auth.UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// principalUser is who the request acts as: the engine's account the
// Principal names, read fresh, or for a key a principal of its own.
func (i *identityHTTPAuthInterceptor) principalUser(ctx context.Context, principal identity.Principal) (entities.User, bool) {
	id, err := uuid.Parse(principal.UserID)
	if err != nil {
		return entities.User{}, false
	}
	if principal.ViaKey {
		return keyPrincipal(id, principal.Roles)
	}
	user, err := i.account(ctx, id)
	if err != nil {
		return entities.User{}, false
	}
	return user, true
}

// keyPrincipal is an integration key as the engine sees it: no account, a
// member of the organizations its Principal names and nothing else, holding
// there the roles it was given and no role globally. Administrator is never
// one of them, whatever the Identity said: a key that leaked must not be a
// way to administer an organization.
func keyPrincipal(keyID uuid.UUID, roles map[string][]string) (entities.User, bool) {
	user := entities.User{
		ID:                  keyID,
		Username:            "integration:" + keyID.String(),
		IntegrationKey:      keyID,
		RolesByOrganization: make(map[uuid.UUID][]string, len(roles)),
	}
	for org, held := range roles {
		orgID, err := uuid.Parse(org)
		if err != nil {
			return entities.User{}, false
		}
		kept := make([]string, 0, len(held))
		for _, role := range held {
			if !strings.EqualFold(role, entities.RoleAdmin) {
				kept = append(kept, role)
			}
		}
		user.RolesByOrganization[orgID] = kept
		user.Organizations = append(user.Organizations, &entities.Organization{ID: orgID})
	}
	if len(user.Organizations) == 0 {
		return entities.User{}, false
	}
	return user, true
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Debug().Err(err).Msg("Could not write the refusal to the caller")
	}
}
