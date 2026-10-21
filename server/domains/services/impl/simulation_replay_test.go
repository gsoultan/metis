package impl

import (
	"testing"
	"time"
)

// A replayed person's time is written the way the designer offers it, so the
// answer shows "1 day" rather than a number of seconds nobody reads.
func TestIsoDurationUsesTheLargestExactUnits(t *testing.T) {
	tests := map[time.Duration]string{
		0:                              "PT0S",
		500 * time.Millisecond:         "PT0S",
		time.Hour:                      "PT1H",
		24 * time.Hour:                 "P1D",
		26*time.Hour + 30*time.Minute:  "P1DT2H30M",
		90 * time.Second:               "PT1M30S",
		7*24*time.Hour + 5*time.Second: "P7DT5S",
	}
	for d, want := range tests {
		if got := isoDuration(d); got != want {
			t.Errorf("isoDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
