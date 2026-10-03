package entities

// DelegationState is where a delegated task stands. Empty for a task that has
// not been delegated.
type DelegationState string

const (
	// DelegationPending: the task is with a delegate, who works on it and
	// hands it back. Nobody completes it until they have.
	DelegationPending DelegationState = "pending"
	// DelegationResolved: the delegate handed it back, and its owner holds it
	// again and completes it.
	DelegationResolved DelegationState = "resolved"
)
