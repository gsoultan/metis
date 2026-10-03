package entities

// DeviationStatus is where a deviation stands.
type DeviationStatus string

const (
	DeviationApplied         DeviationStatus = "applied"
	DeviationPendingApproval DeviationStatus = "pending_approval"
	DeviationRejected        DeviationStatus = "rejected"
	DeviationExpired         DeviationStatus = "expired"
	DeviationStale           DeviationStatus = "stale"
)

// Valid reports whether s is one of the statuses the ledger records.
func (s DeviationStatus) Valid() bool {
	switch s {
	case DeviationApplied, DeviationPendingApproval, DeviationRejected, DeviationExpired, DeviationStale:
		return true
	}
	return false
}

// Live reports whether the deviation still stands or may yet: applied, or
// waiting for the approval that would apply it.
func (s DeviationStatus) Live() bool {
	return s == DeviationApplied || s == DeviationPendingApproval
}
