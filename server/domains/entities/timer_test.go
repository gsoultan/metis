package entities_test

import (
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// BPMN timers are ISO-8601. The designer says so in its own help text —
// "PT1H is one hour, PT10M ten minutes, P1D a day" — and every BPMN file
// imported from another tool writes them that way.
func TestParseTimerExpression(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		expr    string
		want    time.Time
		wantErr bool
	}{
		// ISO-8601 durations — the format the property panel tells users to type.
		{name: "one hour", expr: "PT1H", want: now.Add(time.Hour)},
		{name: "ten minutes", expr: "PT10M", want: now.Add(10 * time.Minute)},
		{name: "thirty seconds", expr: "PT30S", want: now.Add(30 * time.Second)},
		{name: "one day", expr: "P1D", want: now.AddDate(0, 0, 1)},
		{name: "combined date and time", expr: "P1DT2H30M", want: now.AddDate(0, 0, 1).Add(2*time.Hour + 30*time.Minute)},
		{name: "two weeks", expr: "P2W", want: now.AddDate(0, 0, 14)},
		{name: "one month", expr: "P1M", want: now.AddDate(0, 1, 0)},
		{name: "one year", expr: "P1Y", want: now.AddDate(1, 0, 0)},
		{name: "lowercase is accepted", expr: "pt1h", want: now.Add(time.Hour)},
		{name: "fractional seconds", expr: "PT1.5S", want: now.Add(1500 * time.Millisecond)},

		// An absolute instant — the designer's "Until a date".
		{name: "absolute date", expr: "2026-01-01T12:00:00Z", want: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)},

		// Go-style durations kept working so definitions written before this
		// still run.
		{name: "go duration hours", expr: "1h", want: now.Add(time.Hour)},
		{name: "go duration compound", expr: "1h30m", want: now.Add(90 * time.Minute)},

		// A repeating timer needs a rescheduling mechanism the engine does not
		// have. It must say so rather than be read as a plain duration.
		{name: "repeating cycle is refused", expr: "R3/PT10M", wantErr: true},
		{name: "unbounded cycle is refused", expr: "R/PT10M", wantErr: true},

		{name: "empty is an error", expr: "", wantErr: true},
		{name: "nonsense is an error", expr: "soon", wantErr: true},
		{name: "bare P is an error", expr: "P", wantErr: true},
		{name: "duration with no designator is an error", expr: "PT", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := entities.ParseTimerExpression(tc.expr, now)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A cycle expression must name itself in the error, so the message points at
// the thing the designer chose rather than at a parse failure.
func TestParseTimerExpressionExplainsUnsupportedCycles(t *testing.T) {
	_, err := entities.ParseTimerExpression("R3/PT10M", time.Now())
	if err == nil {
		t.Fatal("a repeating timer was accepted")
	}
	if !contains(err.Error(), "repeat") && !contains(err.Error(), "cycle") {
		t.Errorf("the error does not explain that repeating timers are unsupported: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
