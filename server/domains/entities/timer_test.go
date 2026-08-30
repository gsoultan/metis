package entities_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
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

		// A cycle carries a repeat count that a single instant cannot express,
		// so it belongs to ParseTimerSchedule.
		{name: "repeating cycle needs a schedule", expr: "R3/PT10M", wantErr: true},
		{name: "unbounded cycle needs a schedule", expr: "R/PT10M", wantErr: true},

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

// A repeating cycle resolves to a first occurrence plus the repeats still owed.
func TestParseTimerSchedule(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		expr        string
		wantFireAt  time.Time
		wantEvery   time.Duration
		wantRepeats int
		wantRepeat  bool
	}{
		{
			name: "three occurrences leaves two to come", expr: "R3/PT10M",
			wantFireAt: now.Add(10 * time.Minute), wantEvery: 10 * time.Minute,
			wantRepeats: 2, wantRepeat: true,
		},
		{
			name: "a single occurrence does not repeat", expr: "R1/PT10M",
			wantFireAt: now.Add(10 * time.Minute), wantEvery: 10 * time.Minute,
			wantRepeats: 0, wantRepeat: false,
		},
		{
			name: "no count means unbounded", expr: "R/PT1H",
			wantFireAt: now.Add(time.Hour), wantEvery: time.Hour,
			wantRepeats: entities.RepeatsForever, wantRepeat: true,
		},
		{
			name: "a start instant is allowed before the interval", expr: "R2/2026-01-01T00:00:00Z/PT30M",
			wantFireAt: now.Add(30 * time.Minute), wantEvery: 30 * time.Minute,
			wantRepeats: 1, wantRepeat: true,
		},
		{
			name: "a plain duration does not repeat", expr: "PT10M",
			wantFireAt: now.Add(10 * time.Minute), wantEvery: 0,
			wantRepeats: 0, wantRepeat: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := entities.ParseTimerSchedule(tc.expr, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.FireAt.Equal(tc.wantFireAt) {
				t.Errorf("fires at %v, want %v", got.FireAt, tc.wantFireAt)
			}
			if got.Every != tc.wantEvery {
				t.Errorf("interval %v, want %v", got.Every, tc.wantEvery)
			}
			if got.Repeats != tc.wantRepeats {
				t.Errorf("%d repeats remaining, want %d", got.Repeats, tc.wantRepeats)
			}
			if got.IsRepeating() != tc.wantRepeat {
				t.Errorf("IsRepeating() = %v, want %v", got.IsRepeating(), tc.wantRepeat)
			}
		})
	}
}

// Each occurrence hands the next one one fewer repeat, and an unbounded cycle
// stays unbounded.
func TestTimerScheduleNextCountsDown(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	s, err := entities.ParseTimerSchedule("R3/PT10M", now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for want := 1; want >= 0; want-- {
		s = s.Next(now)
		if s.Repeats != want {
			t.Fatalf("expected %d repeats remaining, got %d", want, s.Repeats)
		}
	}
	if s.IsRepeating() {
		t.Error("the schedule still reports repeats after the last occurrence")
	}

	unbounded, err := entities.ParseTimerSchedule("R/PT10M", now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if next := unbounded.Next(now); next.Repeats != entities.RepeatsForever || !next.IsRepeating() {
		t.Errorf("an unbounded cycle stopped repeating: %+v", next)
	}
}
