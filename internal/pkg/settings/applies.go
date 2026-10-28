package settings

// Applies says when a saved value starts to count.
type Applies string

const (
	// AtRestart is read once, when the server starts: a saved value counts
	// from the next restart of each replica.
	AtRestart Applies = "restart"
	// Live is read where it is used, and every replica picks up a saved
	// value within ReloadInterval.
	Live Applies = "live"
)
