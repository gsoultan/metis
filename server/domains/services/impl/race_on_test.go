//go:build race

package impl

// raceDetector reports whether this test binary was built with the race
// detector, under which a count of allocations is not the code's own: the
// detector's instrumentation adds and removes a few from run to run.
const raceDetector = true
