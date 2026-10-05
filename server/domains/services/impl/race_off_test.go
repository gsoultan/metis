//go:build !race

package impl

// raceDetector reports whether this test binary was built with the race
// detector.
const raceDetector = false
