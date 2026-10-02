package entities

import "encoding/json"

// DelegationForClients is the owner and the delegation state a client is
// sent, over REST and over Connect alike.
//
// They are the task's own, except for a pending mark on a task that is not
// waiting to be handed back (AwaitsHandBack): a pod on the release before
// this one claims or releases a task this release delegated, and the engine
// withdraws one, by writing the status alone. The owner and the mark stay on
// a row they no longer describe. Sent as they stood, every client would have
// to know the three-part rule to tell a live delegation from a leftover, and
// that rule is written once, here on the server.
//
// A delegation that was handed back keeps both: its owner is who it came back
// to.
func (t Task) DelegationForClients() (*User, DelegationState) {
	if t.DelegationState == DelegationPending && !t.AwaitsHandBack() {
		return nil, ""
	}
	return t.Owner, t.DelegationState
}

// MarshalJSON is the REST shape of a task: its fields under their tags, with
// the delegation as a client is sent it (DelegationForClients).
//
// It is the client view and nothing else. A type must not embed Task: it
// would inherit this method, and its own fields would be left out of what it
// encodes. And nothing may persist a task through it: a mark this blanks for
// a client is still on the row, and writing the blank back would lose it.
func (t Task) MarshalJSON() ([]byte, error) {
	// A type with Task's fields and none of its methods, so encoding it does
	// not come back here.
	type fields Task
	t.Owner, t.DelegationState = t.DelegationForClients()
	return json.Marshal(fields(t))
}
