package impl

import (
	"strings"
	"testing"
	"time"
)

// The approval window is a setting, not a constant (spec, Slice 3b), and a
// value outside 1h–720h or one that cannot be read is said, never swallowed.
func TestTheApprovalWindowIsReadClampedAndSaid(t *testing.T) {
	cases := []struct {
		raw     string
		want    time.Duration
		problem string
	}{
		{"", 72 * time.Hour, ""},
		{"96h", 96 * time.Hour, ""},
		{"1h", time.Hour, ""},
		{"720h", 720 * time.Hour, ""},
		{"30m", time.Hour, "shorter than 1h"},
		{"2000h", 720 * time.Hour, "longer than 720h"},
		{"soon", 72 * time.Hour, "cannot be read"},
		{"-5h", 72 * time.Hour, "cannot be read"},
		{"0", 72 * time.Hour, "cannot be read"},
	}
	for _, c := range cases {
		t.Setenv(EnvDeviationApprovalTTL, c.raw)
		got, problem := DeviationApprovalTTL()
		if got != c.want || (c.problem == "") != (problem == "") || !strings.Contains(problem, c.problem) {
			t.Errorf("%q: got %v %q, want %v containing %q", c.raw, got, problem, c.want, c.problem)
		}
		if problem != "" && !strings.Contains(problem, EnvDeviationApprovalTTL) {
			t.Errorf("%q: the problem %q does not name the setting", c.raw, problem)
		}
		if problem != "" && !strings.Contains(problem, "requests wait "+hoursOf(c.want)) {
			t.Errorf("%q: the problem %q does not say how long a request then waits", c.raw, problem)
		}
	}
}
