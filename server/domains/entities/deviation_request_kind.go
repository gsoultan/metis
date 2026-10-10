package entities

// DeviationRequestKind is what a second administrator is asked to approve. A
// closed set: a kind the code does not know is a request nobody can carry out.
type DeviationRequestKind string

const (
	// DeviationRequestInstanceWaive is a step of one instance waived in place.
	DeviationRequestInstanceWaive DeviationRequestKind = "instance_waive"
	// DeviationRequestMigration is a migration that skips a step or drops a
	// control running instances have not passed.
	DeviationRequestMigration DeviationRequestKind = "migration"
)

// Valid reports whether k is one of the kinds a request can have.
func (k DeviationRequestKind) Valid() bool {
	return k == DeviationRequestInstanceWaive || k == DeviationRequestMigration
}
