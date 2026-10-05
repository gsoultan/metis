package contracts

// DeviationRequestService is what is done with a request for a second
// administrator once it has been made: it is read, decided, and closed when
// nobody decides it. Making one is the work of whatever asks — the in-place
// command for a waive.
type DeviationRequestService interface {
	DeviationApprover
	DeviationRequestLister
	DeviationRequestExpirer
}
