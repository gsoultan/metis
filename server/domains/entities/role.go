package entities

// Roles carried on a User and in JWT claims.
//
// Setup seeds the first account with RoleAdmin. Roles are uppercase because
// that is what the setup seeder writes and what existing tokens carry;
// HasRole compares case-insensitively so a token minted with "admin" still
// matches.
const (
	// RoleAdmin administers the platform: users, groups, organizations,
	// projects, connectors and deployments.
	RoleAdmin = "ADMIN"

	// RoleDesigner authors process and decision definitions.
	RoleDesigner = "DESIGNER"

	// RoleOperator runs the system day to day: resolves incidents, retrying
	// the step that failed, starts ad hoc tasks and broadcasts signals, and
	// takes the tasks nobody was named for (Task.FallsToOperators).
	// Migrating running instances is an administrator's.
	RoleOperator = "OPERATOR"

	// RoleQueryAuthor may deploy a process with a database lookup in it.
	//
	// A lookup carries SQL its author wrote, run against a database an
	// administrator connected, with whatever that connection's login may read.
	// That is more than designing a process, so it is a permission of its own —
	// held beside RoleDesigner, not instead of it. An administrator has it
	// already.
	RoleQueryAuthor = "QUERY_AUTHOR"

	// RoleUser participates in processes — the task inbox. Every authenticated
	// principal is treated as at least a RoleUser, so endpoints that only need
	// "signed in" should require no roles at all rather than requiring this.
	RoleUser = "USER"
)

// TakesUnnamedWork reports whether roles let their holder take a task nobody
// was named for (Task.FallsToOperators): an administrator or an operator.
func TakesUnnamedWork(roles []string) bool {
	return HasRole(roles, RoleAdmin) || HasRole(roles, RoleOperator)
}

// HasRole reports whether roles contains want, ignoring case.
func HasRole(roles []string, want string) bool {
	for _, r := range roles {
		if equalFoldASCII(r, want) {
			return true
		}
	}
	return false
}

// equalFoldASCII is a case-insensitive comparison for the ASCII role
// vocabulary. strings.EqualFold would also work; this avoids pulling strings
// into the entities package for one call.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		ca, cb := a[i], b[i]
		if 'a' <= ca && ca <= 'z' {
			ca -= 'a' - 'A'
		}
		if 'a' <= cb && cb <= 'z' {
			cb -= 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
