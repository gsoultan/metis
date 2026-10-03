package entities

// PassedOverInstance is one instance a migration left alone: the run did not
// decide its work or move it, and it is still on the version it was running.
type PassedOverInstance struct {
	Instance *ProcessInstance
	// Reason is why, in words for the person who asked for the migration: it
	// names a step as people know it, never by its id.
	Reason string
}
