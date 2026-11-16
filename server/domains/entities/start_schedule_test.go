package entities

import (
	"strings"
	"testing"
	"time"
)

func mustSchedule(t *testing.T, raw any) StartSchedule {
	t.Helper()
	schedule, err := ParseStartSchedule(raw)
	if err != nil {
		t.Fatalf("ParseStartSchedule(%v): %v", raw, err)
	}
	return schedule
}

func instant(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("bad test instant %q: %v", value, err)
	}
	return parsed
}

// Each rule a person can choose names the next time it runs, in its own zone.
func TestAScheduleNamesItsNextRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schedule string
		after    string
		want     string
	}{
		{"every day, later today",
			`{"every":"day","at":"09:00","time_zone":"Asia/Jakarta"}`,
			"2026-11-16T08:00:00+07:00", "2026-11-16T09:00:00+07:00"},
		{"every day, exactly at the time, is tomorrow: a time already used is never given again",
			`{"every":"day","at":"09:00","time_zone":"Asia/Jakarta"}`,
			"2026-11-16T09:00:00+07:00", "2026-11-17T09:00:00+07:00"},
		{"every weekday skips the weekend",
			`{"every":"weekday","at":"09:00","time_zone":"Asia/Jakarta"}`,
			"2026-11-13T10:00:00+07:00", "2026-11-16T09:00:00+07:00"},
		{"every week on the days named",
			`{"every":"week","days":["mon","Wed"],"at":"09:00","time_zone":"Asia/Jakarta"}`,
			"2026-11-16T10:00:00+07:00", "2026-11-18T09:00:00+07:00"},
		{"every month on a day",
			`{"every":"month","day_of_month":1,"at":"00:30","time_zone":"UTC"}`,
			"2026-11-16T00:00:00Z", "2026-12-01T00:30:00Z"},
		{"day 31 in a 30-day month is its last day",
			`{"every":"month","day_of_month":31,"at":"09:00","time_zone":"UTC"}`,
			"2026-11-01T00:00:00Z", "2026-11-30T09:00:00Z"},
		{"day 31 in February of a leap year",
			`{"every":"month","day_of_month":31,"at":"09:00","time_zone":"UTC"}`,
			"2028-02-01T00:00:00Z", "2028-02-29T09:00:00Z"},
		{"the zone decides the day: 09:00 in Jakarta is 02:00 UTC",
			`{"every":"day","at":"09:00","time_zone":"Asia/Jakarta"}`,
			"2026-11-16T01:00:00Z", "2026-11-16T02:00:00Z"},
		{"an interval keeps to its beat, not to when it was asked",
			`{"every":"interval","interval_minutes":15,"time_zone":"UTC"}`,
			"2026-11-16T10:07:31Z", "2026-11-16T10:15:00Z"},
		{"an interval on the beat moves to the next one",
			`{"every":"interval","interval_minutes":120,"time_zone":"UTC"}`,
			"2026-11-16T10:00:00Z", "2026-11-16T12:00:00Z"},
		{"nothing runs before the first date",
			`{"every":"day","at":"09:00","time_zone":"UTC","from":"2026-12-01"}`,
			"2026-11-16T10:00:00Z", "2026-12-01T09:00:00Z"},
		{"an interval starts at the first date's midnight",
			`{"every":"interval","interval_minutes":90,"time_zone":"UTC","from":"2026-12-01"}`,
			"2026-11-16T10:00:00Z", "2026-12-01T00:00:00Z"},
		{"the last date is included",
			`{"every":"day","at":"23:00","time_zone":"UTC","until":"2026-11-16"}`,
			"2026-11-16T10:00:00Z", "2026-11-16T23:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mustSchedule(t, tc.schedule).Next(instant(t, tc.after))
			if !ok {
				t.Fatalf("no next run, want %s", tc.want)
			}
			if want := instant(t, tc.want); !got.Equal(want) {
				t.Fatalf("next run = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
		})
	}
}

// After its last date a schedule has no next run, instead of one in the past
// or one that drifts on for ever.
func TestAScheduleEndsAfterItsLastDate(t *testing.T) {
	for _, schedule := range []string{
		`{"every":"day","at":"09:00","time_zone":"UTC","until":"2026-11-16"}`,
		`{"every":"interval","interval_minutes":60,"time_zone":"UTC","until":"2026-11-16"}`,
	} {
		if next, ok := mustSchedule(t, schedule).Next(instant(t, "2026-11-16T23:30:00Z")); ok {
			t.Fatalf("%s: next run = %s after the last date", schedule, next.Format(time.RFC3339))
		}
	}
}

// New York, 8 March 2026: 02:00 to 03:00 does not happen. A run set for 02:30
// starts at the next minute that exists, once, and is back at 02:30 the day
// after.
func TestATimeTheClocksSkipRunsAtTheNextMinuteThatExists(t *testing.T) {
	schedule := mustSchedule(t, `{"every":"day","at":"02:30","time_zone":"America/New_York"}`)

	first, ok := schedule.Next(instant(t, "2026-03-07T12:00:00-05:00"))
	if want := instant(t, "2026-03-08T03:00:00-04:00"); !ok || !first.Equal(want) {
		t.Fatalf("run on the day the clocks go forward = %s, want %s", first.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	second, ok := schedule.Next(first)
	if want := instant(t, "2026-03-09T02:30:00-04:00"); !ok || !second.Equal(want) {
		t.Fatalf("run the day after = %s, want %s", second.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// New York, 1 November 2026: 01:30 happens twice. The run starts at the first
// of them, and the next run is the following day, not an hour later.
func TestATimeThatHappensTwiceRunsOnce(t *testing.T) {
	schedule := mustSchedule(t, `{"every":"day","at":"01:30","time_zone":"America/New_York"}`)

	first, ok := schedule.Next(instant(t, "2026-10-31T12:00:00-04:00"))
	if want := instant(t, "2026-11-01T01:30:00-04:00"); !ok || !first.Equal(want) {
		t.Fatalf("run on the day the clocks go back = %s, want %s", first.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	second, ok := schedule.Next(first)
	if want := instant(t, "2026-11-02T01:30:00-05:00"); !ok || !second.Equal(want) {
		t.Fatalf("run the day after = %s, want %s", second.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// Walking a schedule forward never repeats or goes back, across a year that
// includes both clock changes.
func TestNextRunsOnlyMoveForward(t *testing.T) {
	for _, raw := range []string{
		`{"every":"day","at":"02:30","time_zone":"America/New_York"}`,
		`{"every":"weekday","at":"01:30","time_zone":"Europe/London"}`,
		`{"every":"month","day_of_month":31,"at":"23:59","time_zone":"Australia/Lord_Howe"}`,
		`{"every":"interval","interval_minutes":1440,"time_zone":"America/New_York"}`,
	} {
		schedule := mustSchedule(t, raw)
		at := instant(t, "2026-01-01T00:00:00Z")
		for range 400 {
			next, ok := schedule.Next(at)
			if !ok {
				t.Fatalf("%s: ran out after %s", raw, at.Format(time.RFC3339))
			}
			if !next.After(at) {
				t.Fatalf("%s: next run %s is not after %s", raw, next.Format(time.RFC3339), at.Format(time.RFC3339))
			}
			at = next
		}
	}
}

// Anything that is not one of the offered choices is refused, with the reason.
func TestAScheduleThatCannotRunIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, schedule, reason string
	}{
		{"not an object", `"0 9 * * *"`, "not a JSON object"},
		{"a cron expression", `0 9 * * *`, "not a JSON object"},
		{"no rule", `{"time_zone":"UTC"}`, "how often"},
		{"an unknown rule", `{"every":"fortnight","time_zone":"UTC"}`, "fortnight"},
		{"no zone", `{"every":"day","at":"09:00"}`, "no time zone"},
		{"the server's own zone", `{"every":"day","at":"09:00","time_zone":"Local"}`, "Local"},
		{"an unknown zone", `{"every":"day","at":"09:00","time_zone":"Mars/Olympus"}`, "Mars/Olympus"},
		{"an interval of nothing", `{"every":"interval","interval_minutes":0,"time_zone":"UTC"}`, "every 0 minutes"},
		{"a negative interval", `{"every":"interval","interval_minutes":-5,"time_zone":"UTC"}`, "every -5 minutes"},
		{"an interval past a year", `{"every":"interval","interval_minutes":9999999,"time_zone":"UTC"}`, "one year"},
		{"no time of day", `{"every":"day","time_zone":"UTC"}`, "time of day"},
		{"a time that is not one", `{"every":"day","at":"25:00","time_zone":"UTC"}`, "25:00"},
		{"a week with no day", `{"every":"week","at":"09:00","time_zone":"UTC"}`, "no day of the week"},
		{"a day that is not one", `{"every":"week","days":["funday"],"at":"09:00","time_zone":"UTC"}`, "funday"},
		{"day 0 of the month", `{"every":"month","day_of_month":0,"at":"09:00","time_zone":"UTC"}`, "day 0"},
		{"day 32 of the month", `{"every":"month","day_of_month":32,"at":"09:00","time_zone":"UTC"}`, "day 32"},
		{"a first date that is not one", `{"every":"day","at":"09:00","time_zone":"UTC","from":"tomorrow"}`, "tomorrow"},
		{"a last date before the first", `{"every":"day","at":"09:00","time_zone":"UTC","from":"2026-12-02","until":"2026-12-01"}`, "before its first date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStartSchedule(tc.schedule)
			if err == nil {
				t.Fatalf("%s was accepted", tc.schedule)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("refusal %q does not mention %q", err, tc.reason)
			}
		})
	}
}

// The designer saves the schedule as an object on the start step; a step
// without one has no schedule, and that is not an error.
func TestAStartStepsScheduleIsReadFromItsProperties(t *testing.T) {
	scheduled := &Node{ID: "start", Type: StartEvent, Properties: map[string]any{
		StartScheduleProperty: map[string]any{"every": "week", "days": []any{"fri"}, "at": "17:00", "time_zone": "Asia/Jakarta"},
	}}
	schedule, ok, err := StartScheduleOf(scheduled)
	if err != nil || !ok {
		t.Fatalf("StartScheduleOf = ok %v, err %v", ok, err)
	}
	next, _ := schedule.Next(instant(t, "2026-11-16T00:00:00+07:00"))
	if want := instant(t, "2026-11-20T17:00:00+07:00"); !next.Equal(want) {
		t.Fatalf("next run = %s, want %s", next.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	for _, plain := range []*Node{nil, {ID: "start"}, {ID: "start", Properties: map[string]any{StartScheduleProperty: ""}}} {
		if _, ok, err := StartScheduleOf(plain); ok || err != nil {
			t.Fatalf("a step with no schedule read as ok %v, err %v", ok, err)
		}
	}

	broken := &Node{ID: "start", Properties: map[string]any{StartScheduleProperty: map[string]any{"every": "day"}}}
	if _, ok, err := StartScheduleOf(broken); !ok || err == nil {
		t.Fatalf("a broken schedule read as ok %v, err %v", ok, err)
	}
}
