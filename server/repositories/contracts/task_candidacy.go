package contracts

// Candidacy is somebody looking for work to pick up: what the inbox's
// "Available to Claim" is asked for.
type Candidacy struct {
	// User is who is asking, matched against a task's candidate users.
	User string

	// Groups are the groups they are in, by name and by id, matched against a
	// task's candidate groups.
	Groups []string

	// Unnamed adds the unclaimed tasks nobody was named for — no assignee and
	// no candidates. Only an administrator or an operator may take one, unless
	// the installation has brought back the rule that let anybody, so the
	// caller says whether this person may.
	Unnamed bool
}
