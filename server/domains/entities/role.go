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

	// RoleOperator runs the system day to day: resolves incidents, retries
	// jobs, migrates instances.
	RoleOperator = "OPERATOR"

	// RoleUser participates in processes — the task inbox. Every authenticated
	// principal is treated as at least a RoleUser, so endpoints that only need
	// "signed in" should require no roles at all rather than requiring this.
	RoleUser = "USER"
)

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
