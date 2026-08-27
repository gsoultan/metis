//go:build !race

package slo

// raceEnabled reports whether this binary was built with the race detector.
// See race_on.go.
const raceEnabled = false
