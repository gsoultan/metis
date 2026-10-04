package entities

import "github.com/google/uuid"

const (
	// MaxDeviationOutputs is the most values one waive may set. A step's form
	// has a handful of fields; a request naming more is not a waiver.
	MaxDeviationOutputs = 50
	// MaxDeviationOutputBytes is the most a waive's values may weigh, encoded
	// as JSON. They are kept with the record of the waive.
	MaxDeviationOutputBytes = 64 << 10
)

// DeviationCommand asks for one instance to be dealt with where it stands,
// outside what its process says: a step waived, the instance cancelled, or the
// instance held for somebody to decide.
//
// No JSON or ORM tags: an endpoint maps a request to it.
type DeviationCommand struct {
	InstanceID uuid.UUID
	// Kind is waive, cancel or hold.
	Kind DeviationKind
	// NodeID is the step a waive or a hold acts on, and the step a cancel ends
	// the instance at. A cancel may leave it empty: that is how an instance
	// that waits at no step is closed.
	NodeID string
	// Reason is why, in the words of whoever asks. It is kept with the record.
	Reason string
	// Outputs is what a waived step counts as: values for fields its form
	// declares, set as though somebody had filled them in. A waive only.
	Outputs map[string]any
	// VisitKey is the visit key of the plan the caller previewed. An apply
	// names it, and is refused when the work is no longer what was previewed.
	VisitKey string
	// DryRun asks for the plan and nothing else.
	DryRun bool
}
