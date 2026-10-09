package entities

import "slices"

// PassedOverCause is why a migration left an instance alone, as a code a
// client can translate. A closed set: one value for each sentence a run can
// give (the migration service's migration_passed_over.go), and nothing else.
//
// The sentence beside it is English and stays; the cause is for an interface
// that speaks another language, and for a caller that acts on the answer
// without reading prose. A new way of being passed over adds a value here, a
// sentence there, and words for it in every catalogue the interface ships
// (tests/roledrift fails on a cause that has none).
type PassedOverCause string

const (
	// PassedOverLeftTheStep: a skip, a cancel or a hold found the instance,
	// once locked, no longer waiting at the step it decides. Names that step.
	PassedOverLeftTheStep PassedOverCause = "left_the_step"
	// PassedOverNoLongerRunning: the instance finished, or was ended, between
	// the run listing it and locking it. Names no step.
	PassedOverNoLongerRunning PassedOverCause = "no_longer_running"
	// PassedOverNotPlannedFor: the instance was not on the source version when
	// the migration was planned. Names no step.
	PassedOverNotPlannedFor PassedOverCause = "not_planned_for"
	// PassedOverAlreadyMoved: once locked, the instance was no longer on the
	// source version: another run of a migration had moved it. Names no step.
	PassedOverAlreadyMoved PassedOverCause = "already_moved"
	// PassedOverNowhereToLand: once locked, the instance held work on steps
	// the new version has no step for and the mapping does not cover. Names
	// those steps.
	PassedOverNowhereToLand PassedOverCause = "nowhere_to_land"
	// PassedOverLeftWhereNothingDecides: once locked, the instance had an open
	// task or a waiting event, and no token, on steps this migration decides.
	// Names those steps.
	PassedOverLeftWhereNothingDecides PassedOverCause = "left_where_nothing_decides"
	// PassedOverWaitingToBeDecided: once locked, the instance had a token on
	// steps this migration decides, and no decision had settled it. Names
	// those steps.
	PassedOverWaitingToBeDecided PassedOverCause = "waiting_to_be_decided"
	// PassedOverCountersWouldMerge: once locked, the instance held progress
	// counters on two steps the mapping puts onto one. Names no step.
	PassedOverCountersWouldMerge PassedOverCause = "counters_would_merge"
)

// PassedOverCauses is every cause a run can give, sorted.
func PassedOverCauses() []PassedOverCause {
	return []PassedOverCause{
		PassedOverAlreadyMoved,
		PassedOverCountersWouldMerge,
		PassedOverLeftTheStep,
		PassedOverLeftWhereNothingDecides,
		PassedOverNoLongerRunning,
		PassedOverNotPlannedFor,
		PassedOverNowhereToLand,
		PassedOverWaitingToBeDecided,
	}
}

// Valid reports whether c is one of the causes a run can give.
func (c PassedOverCause) Valid() bool {
	return slices.Contains(PassedOverCauses(), c)
}
