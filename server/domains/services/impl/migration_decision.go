package impl

// decision is what a migration's node actions made of one instance.
type decision struct {
	// outcome is what the run does with the instance next.
	outcome decisionOutcome
	// left is the step an action found the instance no longer on. Set with
	// outcomeMovedOn, so the run can say which step it was.
	left string
	// wrote is whether an action changed the instance. True for one that was
	// cancelled, held or skipped — and for one a skip advanced before a second
	// skip found it gone from its own step, which is passed over and changed
	// both.
	wrote bool
}
