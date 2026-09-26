package interceptors

import (
	"slices"
	"sync"

	"github.com/gsoultan/metis/server/domains/entities"
)

// roleRegistry records which roles each gated method requires, as its chain
// is built.
//
// What a role allows was answered by one hand-written sentence per role, which
// nothing held to the gates. A chain is a closure and cannot be asked
// afterwards what it checks, so the answer is written down at the one moment
// the method and its roles are both in hand: when ProtectedChainWithRoles
// builds the gate.
//
// Bounded by the code, not by requests: one entry per gated method, all of
// them written while the endpoints are built at startup.
type roleRegistry struct {
	mu       sync.Mutex
	required map[string][]string
}

func newRoleRegistry() *roleRegistry {
	return &roleRegistry{required: map[string][]string{}}
}

// record notes that method requires one of roles. A chain that requires no
// role only proves somebody is signed in, so it gates nothing and is not
// recorded.
func (r *roleRegistry) record(method string, roles []string) {
	if len(roles) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.required[method] = slices.Clone(roles)
}

// access lists every role with the actions it is required for.
//
// The built-in roles come first, in their own order, each listed even when no
// gate names it: the query author is checked where a process is deployed,
// not by a gate, and "required for nothing here" is an answer. A role a gate
// names that is not built in follows, by name, because leaving it out would
// hide a gate — the drift this exists to prevent.
func (r *roleRegistry) access() []entities.RoleAccess {
	r.mu.Lock()
	defer r.mu.Unlock()
	roles := r.roles()
	access := make([]entities.RoleAccess, 0, len(roles))
	for _, role := range roles {
		access = append(access, entities.RoleAccess{Role: role, Actions: r.actionsFor(role)})
	}
	return access
}

// roles is the built-in roles, then any other role a gate names.
func (r *roleRegistry) roles() []string {
	var builtIn []string
	for _, role := range entities.BuiltInPlatformRoles() {
		builtIn = append(builtIn, role.Name)
	}
	var others []string
	for _, required := range r.required {
		for _, role := range required {
			if !entities.HasRole(builtIn, role) && !entities.HasRole(others, role) {
				others = append(others, role)
			}
		}
	}
	slices.Sort(others)
	return append(builtIn, others...)
}

// actionsFor is every method role is required for, in the legend's order, so
// the answer is the same whichever order the gates were built in.
func (r *roleRegistry) actionsFor(role string) []entities.RoleAction {
	actions := []entities.RoleAction{}
	for method, required := range r.required {
		if entities.HasRole(required, role) {
			actions = append(actions, entities.NewRoleAction(method))
		}
	}
	slices.SortFunc(actions, entities.CompareRoleActions)
	return actions
}
