package entities

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// iso8601Duration matches an ISO-8601 duration: PnYnMnWnD then optionally
// TnHnMnS. Minutes appear in both halves and mean different things depending on
// which side of the T they fall, which is why the two halves are captured
// separately rather than with one set of groups.
// The match is case-insensitive: the standard specifies uppercase designators,
// but rejecting "pt1h" helps nobody, and no Go duration starts with P.
var iso8601Duration = regexp.MustCompile(
	`(?i)^P(?:(\d+(?:[.,]\d+)?)Y)?(?:(\d+(?:[.,]\d+)?)M)?(?:(\d+(?:[.,]\d+)?)W)?(?:(\d+(?:[.,]\d+)?)D)?` +
		`(?:T(?:(\d+(?:[.,]\d+)?)H)?(?:(\d+(?:[.,]\d+)?)M)?(?:(\d+(?:[.,]\d+)?)S)?)?$`)

// TimerSchedule is when a timer fires and, for a repeating one, how often it
// fires again.
type TimerSchedule struct {
	// FireAt is the next occurrence.
	FireAt time.Time
	// Every is the gap between occurrences; zero when the timer fires once.
	Every time.Duration
	// Repeats is how many occurrences are still to come after this one.
	// Zero means this is the last, and RepeatsForever means unbounded.
	Repeats int
}

// RepeatsForever marks a cycle written without a repeat count ("R/PT10M").
const RepeatsForever = -1

// IsRepeating reports whether another occurrence follows this one.
func (s TimerSchedule) IsRepeating() bool {
	return s.Every > 0 && (s.Repeats > 0 || s.Repeats == RepeatsForever)
}

// Next returns the schedule for the occurrence after this one.
func (s TimerSchedule) Next(now time.Time) TimerSchedule {
	next := TimerSchedule{FireAt: now.Add(s.Every), Every: s.Every, Repeats: s.Repeats}
	if s.Repeats > 0 {
		next.Repeats = s.Repeats - 1
	}
	return next
}

// ParseTimerSchedule resolves any BPMN timer expression, including a repeating
// cycle, relative to now.
//
// BPMN 2.0 §10.3.5 defines three forms: timeDuration ("PT1H"), timeDate
// ("2026-01-01T12:00:00Z") and timeCycle ("R3/PT10M", "R/PT10M"). A cycle is
// what a non-interrupting boundary timer uses to nag every so often while an
// activity runs.
func ParseTimerSchedule(expr string, now time.Time) (TimerSchedule, error) {
	trimmed := strings.TrimSpace(expr)
	if repeats, every, ok := parseRepeatingCycle(trimmed); ok {
		if every <= 0 {
			return TimerSchedule{}, fmt.Errorf("timer %q repeats on an empty interval", expr)
		}
		// The count in "R3/PT10M" is the number of occurrences, so the first one
		// leaves that many minus one still to come.
		remaining := repeats
		if repeats > 0 {
			remaining = repeats - 1
		}
		return TimerSchedule{FireAt: now.Add(every), Every: every, Repeats: remaining}, nil
	}

	fireAt, err := ParseTimerExpression(trimmed, now)
	if err != nil {
		return TimerSchedule{}, err
	}
	return TimerSchedule{FireAt: fireAt}, nil
}

// parseRepeatingCycle splits "R<n>/<duration>" into its count and interval.
// A missing count means unbounded.
func parseRepeatingCycle(expr string) (repeats int, every time.Duration, ok bool) {
	if !isRepeatingCycle(expr) {
		return 0, 0, false
	}
	rest := expr[1:]
	slash := strings.IndexByte(rest, '/')
	countPart, durationPart := rest[:slash], rest[slash+1:]

	repeats = RepeatsForever
	if countPart != "" {
		n, err := strconv.Atoi(countPart)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		repeats = n
	}

	// An interval may itself carry a start instant ("R3/2026-01-01T00:00:00Z/PT10M");
	// the trailing element is the duration either way.
	if idx := strings.LastIndexByte(durationPart, '/'); idx >= 0 {
		durationPart = durationPart[idx+1:]
	}
	d, isDuration := parseISO8601Duration(strings.TrimSpace(durationPart))
	if !isDuration {
		return 0, 0, false
	}
	base := time.Time{}
	return repeats, d.addTo(base).Sub(base), true
}

// ParseTimerExpression resolves a BPMN timer expression to the instant it should
// fire, relative to now. A repeating cycle is rejected here — use
// ParseTimerSchedule, which carries the repeat information a cycle needs.
//
// BPMN 2.0 timers are ISO-8601 (§10.3.5): a duration (timeDuration, "PT1H"), an
// instant (timeDate, "2026-01-01T12:00:00Z") or a repeating cycle (timeCycle,
// "R3/PT10M"). Go's time.ParseDuration understands none of those — it reads
// "1h30m" — so it silently rejected every conformant timer, including the exact
// examples the designer's own help text tells users to type.
//
// Go-style durations are still accepted so definitions written against the old
// behaviour keep running.
//
// Years and months are added as calendar units rather than fixed multiples of
// 24h, so "P1M" lands on the same day of the next month and daylight-saving
// shifts do not accumulate drift.
func ParseTimerExpression(expr string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("timer expression is empty")
	}

	// A cycle carries a repeat count that a single instant cannot express.
	if isRepeatingCycle(trimmed) {
		return time.Time{}, fmt.Errorf(
			"timer %q is a repeating cycle; resolve it with ParseTimerSchedule", expr)
	}

	if d, ok := parseISO8601Duration(trimmed); ok {
		return d.addTo(now), nil
	}

	// timeDate: an absolute instant.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, nil
		}
	}

	// Go-style duration, for definitions predating ISO-8601 support.
	if d, err := time.ParseDuration(trimmed); err == nil {
		return now.Add(d), nil
	}

	return time.Time{}, fmt.Errorf(
		"timer %q is not a recognised ISO-8601 duration (PT1H), date (2026-01-01T12:00:00Z) or Go duration (1h30m)", expr)
}

// isRepeatingCycle reports whether expr is an ISO-8601 repeating interval,
// which starts with R optionally followed by a repeat count and a slash.
func isRepeatingCycle(expr string) bool {
	if len(expr) < 2 || (expr[0] != 'R' && expr[0] != 'r') {
		return false
	}
	rest := expr[1:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return false
	}
	// Everything between R and / must be the repeat count, if present at all.
	for _, c := range rest[:slash] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isoDuration holds a parsed ISO-8601 duration. Calendar and clock parts are
// kept apart because only the clock part is a fixed span of time.
type isoDuration struct {
	years, months, days int
	clock               time.Duration
}

func (d isoDuration) addTo(t time.Time) time.Time {
	if d.years != 0 || d.months != 0 || d.days != 0 {
		t = t.AddDate(d.years, d.months, d.days)
	}
	return t.Add(d.clock)
}

// parseISO8601Duration parses "P1DT2H30M" and reports whether expr was a
// duration at all, so the caller can fall through to the other formats.
func parseISO8601Duration(expr string) (isoDuration, bool) {
	m := iso8601Duration.FindStringSubmatch(expr)
	if m == nil {
		return isoDuration{}, false
	}
	// "P" and "PT" match the pattern with every group empty; neither is a
	// duration.
	if !hasAnyComponent(m[1:]) {
		return isoDuration{}, false
	}

	years, yearFrac := splitNumber(m[1])
	months, monthFrac := splitNumber(m[2])
	weeks, weekFrac := splitNumber(m[3])
	days, dayFrac := splitNumber(m[4])
	hours := parseFloat(m[5])
	minutes := parseFloat(m[6])
	seconds := parseFloat(m[7])

	// Fractional calendar units have no exact answer, so they are approximated
	// against their conventional lengths; whole units stay exact calendar maths.
	clock := time.Duration(
		yearFrac*365*24*float64(time.Hour) +
			monthFrac*30*24*float64(time.Hour) +
			(weekFrac*7+dayFrac)*24*float64(time.Hour) +
			hours*float64(time.Hour) +
			minutes*float64(time.Minute) +
			seconds*float64(time.Second),
	)

	return isoDuration{
		years:  years,
		months: months,
		days:   weeks*7 + days,
		clock:  clock,
	}, true
}

func hasAnyComponent(groups []string) bool {
	for _, g := range groups {
		if g != "" {
			return true
		}
	}
	return false
}

// splitNumber returns the whole part of an ISO-8601 numeric field and any
// fractional remainder, so whole units can use calendar arithmetic.
func splitNumber(raw string) (int, float64) {
	v := parseFloat(raw)
	whole := int(v)
	return whole, v - float64(whole)
}

func parseFloat(raw string) float64 {
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	return v
}
