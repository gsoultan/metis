package entities

// DeviationRequestQuery is one page of a project's requests, newest first,
// optionally only those at one status.
type DeviationRequestQuery struct {
	Project *Project
	// Status is the status to list; empty lists every status.
	Status         DeviationRequestStatus
	Page, PageSize int
}
