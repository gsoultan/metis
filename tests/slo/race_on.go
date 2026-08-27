//go:build race

package slo

// raceEnabled reports whether this binary was built with the race detector.
//
// There is no runtime way to ask, so it is a pair of build-tagged constants —
// the idiom the standard library itself uses for the same question.
const raceEnabled = true
