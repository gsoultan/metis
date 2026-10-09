package entities

// MaxPassedOverSteps is how many of the steps a cause is about a run lists
// for one instance. An instance can hold work on as many steps as its version
// has; the count of them all is kept beside the list (StepsInAll).
const MaxPassedOverSteps = 10

// PassedOverStep is a step the cause of passing an instance over is about.
//
// Both are what the author of a definition wrote, and neither has a length
// there: each is kept to the 255 characters a step's name is kept to
// wherever it is recorded or shown.
type PassedOverStep struct {
	// NodeID is the step's id in the version the instance is running.
	NodeID string
	// Name is the name that version gives the step, and its id where it gives
	// none: what the sentence beside the cause calls it.
	Name string
}
