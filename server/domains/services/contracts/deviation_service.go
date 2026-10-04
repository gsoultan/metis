package contracts

// DeviationService is what a request reaches: the ledger as it is read, and
// the command that deals with one running instance where it stands. Recording
// a deviation is not here: every writer records through DeviationRecorder
// inside its own change.
type DeviationService interface {
	DeviationReader
	InstanceDeviator
}
