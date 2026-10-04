package impl

// decisionOutcome is what a migration's node actions made of one instance, and
// so what the run does with it next.
type decisionOutcome int

const (
	// outcomeMove: no action ended the instance's part in the run. It goes on
	// to the rewrite, which moves it to the new version unless it finds, under
	// the instance's lock, that the instance holds work that cannot land there,
	// or that another run has already moved it off the source version.
	outcomeMove decisionOutcome = iota
	// outcomeDealtWith: an action settled it — cancelled, held, or finished by
	// a skip. It is not moved, and it counts among the instances the run has
	// dealt with.
	outcomeDealtWith
	// outcomeMovedOn: the instance is no longer where the listing said. Found
	// under its lock, it had finished, or had left a step the run was about to
	// skip, cancel at or hold at. The run leaves it as it is — not decided, not
	// moved, nothing recorded — on the version it is running, where the next
	// run of the same migration finds it and plans for where it now stands.
	outcomeMovedOn
)
