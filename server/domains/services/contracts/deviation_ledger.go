package contracts

// DeviationLedger is the whole ledger: the services that make a change write it,
// and the timeline reads it.
type DeviationLedger interface {
	DeviationRecorder
	DeviationReader
}
