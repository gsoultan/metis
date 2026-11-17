package auth

import (
	"net/http"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// sessionOnlyPaths are the parts of the API an integration API key is refused
// on whatever roles it holds: accounts and who may do what, the keys
// themselves, and the installation's own configuration. A key is a password
// nobody types and no second factor guards, so what it can reach must not
// include changing who can reach anything. Each is matched with everything
// beneath it.
//
// The roles a key may be given already keep it from most of these. This list
// does not rest on that: a role gaining a permission later must not give it
// to every key that holds the role.
var sessionOnlyPaths = []string{
	"/api/v1/users",
	"/api/v1/integration-keys",
	"/api/v1/platform-users",
	"/api/v1/organizations",
	"/api/v1/groups",
	"/api/v1/settings",
	"/api/v1/upgrades",
	"/api/v1/logout",
}

// refusedToIntegrationKey reports whether principal is an integration API key
// and path is one it may not use.
func refusedToIntegrationKey(principal any, path string) bool {
	u, ok := principal.(entities.User)
	if !ok || !u.ViaIntegrationKey() {
		return false
	}
	for _, p := range sessionOnlyPaths {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// errKeyNotHere is what an integration API key is told on a session-only
// path.
const errKeyNotHere = "An integration API key cannot be used here; an administrator signs in to do this."

func refuseIntegrationKey(w http.ResponseWriter) {
	http.Error(w, errKeyNotHere, http.StatusForbidden)
}
