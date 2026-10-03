package entities

// MigrationResult is what applying a migration did, as distinct from the plan
// it was applied from.
//
// The plan is worked out from one reading of the instances and the apply takes
// them one at a time afterwards, so the two can differ: an instance that was
// waiting at a step when the plan was made may have left it, or finished, by
// the time the apply holds its lock. The apply leaves such an instance alone,
// and this is where it says so. Without it "applied" was the whole answer, and
// it was given for a run that had left an instance running the old version.
type MigrationResult struct {
	// Changed is how many instances the run acted on: moved to the new version,
	// or settled by a skip, a cancel or a hold. An instance found already held
	// counts, because the hold stands.
	Changed int

	// PassedOver are the instances the run left as they were, each with why.
	// None is the ordinary case.
	PassedOver []PassedOverInstance
}
