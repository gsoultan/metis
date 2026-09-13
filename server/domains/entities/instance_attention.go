package entities

import "github.com/google/uuid"

// InstanceAttention is what a list of instances needs in order to say whether
// anything is wrong.
//
// It is a separate idea from ProcessStatus, and has to be, because the engine
// never marks an instance failed. A job that runs out of retries raises an
// incident and stops; the instance stays `active`, which is correct — it has
// not failed, it is waiting for somebody to look at it. A screen that read the
// status column to answer "is anything broken?" would answer "no" for ever.
type InstanceAttention struct {
	// OpenIncidents is how many unresolved incidents each instance on the page
	// holds, keyed by instance id. Absent means none.
	OpenIncidents map[uuid.UUID]int64

	// Total is how many of the project's instances hold at least one, counted
	// across the whole project rather than the page.
	Total int64
}

// NeedsAttention reports whether one instance is waiting on a person.
func (a InstanceAttention) NeedsAttention(instanceID uuid.UUID) bool {
	return a.OpenIncidents[instanceID] > 0
}
