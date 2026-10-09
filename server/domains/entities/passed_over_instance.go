package entities

// PassedOverInstance is one instance a migration left alone: the run did not
// decide its work or move it, and it is still on the version it was running.
type PassedOverInstance struct {
	Instance *ProcessInstance
	// Cause is why, as a code: one of a closed set, for a client that says it
	// in its own language or acts on it.
	Cause PassedOverCause
	// Steps are the steps the cause is about, in the order the sentence names
	// them. None for a cause that is about no step.
	Steps []PassedOverStep
	// Reason is why, in words for the person who asked for the migration: it
	// names a step as people know it, never by its id.
	Reason string
}
