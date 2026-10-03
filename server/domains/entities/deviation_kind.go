package entities

// DeviationKind is what was done to an instance. A closed set: a kind the code
// does not know is a row nobody can read back with confidence.
type DeviationKind string

const (
	DeviationWaive           DeviationKind = "waive"
	DeviationCancel          DeviationKind = "cancel"
	DeviationHold            DeviationKind = "hold"
	DeviationControlWaived   DeviationKind = "control_waived"
	DeviationReassign        DeviationKind = "reassign"
	DeviationDelegate        DeviationKind = "delegate"
	DeviationResolve         DeviationKind = "resolve"
	DeviationRelease         DeviationKind = "release"
	DeviationTaskEdit        DeviationKind = "task_edit"
	DeviationAdHocActivation DeviationKind = "adhoc_activation"
)

// Valid reports whether k is one of the kinds the ledger records.
func (k DeviationKind) Valid() bool {
	switch k {
	case DeviationWaive, DeviationCancel, DeviationHold, DeviationControlWaived,
		DeviationReassign, DeviationDelegate, DeviationResolve, DeviationRelease,
		DeviationTaskEdit, DeviationAdHocActivation:
		return true
	}
	return false
}

// ReasonRequired reports whether a person must say why. True for every kind
// but control_waived, which the system records because a migration dropped a
// hold nobody was asked about, and adhoc_activation, which is the process
// working as designed.
func (k DeviationKind) ReasonRequired() bool {
	return k.Valid() && k != DeviationControlWaived && k != DeviationAdHocActivation
}

// ReasonForbidden reports whether a reason must not be given: control_waived is
// the system's record, and a person's words in it would be taken as theirs.
func (k DeviationKind) ReasonForbidden() bool {
	return k == DeviationControlWaived
}
