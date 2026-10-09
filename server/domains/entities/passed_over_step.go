package entities

// PassedOverStep is a step the cause of passing an instance over is about.
type PassedOverStep struct {
	// NodeID is the step's id in the version the instance is running.
	NodeID string
	// Name is the name that version gives the step, and its id where it gives
	// none: what the sentence beside the cause calls it.
	Name string
}
