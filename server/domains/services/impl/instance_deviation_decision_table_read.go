package impl

// decisionTableRead is what one table reads by itself, before the decisions
// it requires are followed.
type decisionTableRead struct {
	names, requires []string
	// analysable is false when part of the table could not be read, and when
	// the table was not read at all because the plan had read its fill.
	analysable bool
	// found is false when there is no such decision, or no such version.
	found bool
}
