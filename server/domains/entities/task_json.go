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
func (t Task) MarshalJSON() ([]byte, error) {
	// A type with Task's fields and none of its methods, so encoding it does
	// not come back here.
	type fields Task
	t.Owner, t.DelegationState = t.DelegationForClients()
	return json.Marshal(fields(t))
}
