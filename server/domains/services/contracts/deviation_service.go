package contracts

// DeviationService is what a request reaches: the ledger as it is read. What
// changes an instance is added to it where it is built, not here.
type DeviationService interface {
	DeviationReader
}
