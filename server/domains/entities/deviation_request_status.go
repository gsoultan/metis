package entities

// DeviationRequestStatus is where a request for a second administrator's
// approval stands. A closed set: a status the code does not know is a decision
// nobody reads back.
type DeviationRequestStatus string

const (
	// DeviationRequestPending waits for a second administrator.
	DeviationRequestPending DeviationRequestStatus = "pending_approval"
	// DeviationRequestApproved was approved and is being carried out. Never
	// where a request rests: see DeviationRequest.RunWindowClosed.
	DeviationRequestApproved DeviationRequestStatus = "approved"
	// DeviationRequestApplied was approved and carried out.
	DeviationRequestApplied DeviationRequestStatus = "applied"
	// DeviationRequestInterrupted was approved, and its run failed part-way or
	// never reported back.
	DeviationRequestInterrupted DeviationRequestStatus = "interrupted"
	// DeviationRequestStale no longer holds: what it asked for changed before
	// anybody approved it.
	DeviationRequestStale DeviationRequestStatus = "stale"
	// DeviationRequestRejected was refused, or withdrawn by whoever asked.
	DeviationRequestRejected DeviationRequestStatus = "rejected"
	// DeviationRequestExpired was decided by nobody before its deadline.
	DeviationRequestExpired DeviationRequestStatus = "expired"
)

// Valid reports whether s is one of the statuses a request can have.
func (s DeviationRequestStatus) Valid() bool {
	return s.Live() || s.Terminal()
}

// Live reports whether the request still holds its visit or its migration:
// waiting for a decision, or approved and being carried out. One live request
// per visit and per migration is what the database enforces.
func (s DeviationRequestStatus) Live() bool {
	return s == DeviationRequestPending || s == DeviationRequestApproved
}

// Terminal reports whether the request is over: it holds nothing any longer,
// and the same thing may be asked for afresh. Nobody decides it again. One of
// these can still change, though: an interrupted request whose run was in
// fact still going is reported on by that run when it ends — applied, or
// interrupted with what it did — and that report stands over the sweep's.
func (s DeviationRequestStatus) Terminal() bool {
	switch s {
	case DeviationRequestApplied, DeviationRequestInterrupted, DeviationRequestStale,
		DeviationRequestRejected, DeviationRequestExpired:
		return true
	}
	return false
}
