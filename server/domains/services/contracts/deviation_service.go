package contracts

// DeviationService is what a request reaches: the ledger as it is read, the
// command that deals with one running instance where it stands, and the
// requests that wait for a second administrator. Recording a deviation is not
// here: every writer records through DeviationRecorder inside its own change.
type DeviationService interface {
	DeviationReader
	InstanceDeviator
	DeviationRequestService
}
