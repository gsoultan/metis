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

// ParseTimerExpression resolves a BPMN timer expression to the instant it should
// fire, relative to now.
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

	// A repeating cycle needs the job to be re-enqueued after each firing, which
	// the engine has no mechanism for. Reading "R3/PT10M" as a one-shot timer
	// would fire once and look like it worked, so it is refused by name.
	if isRepeatingCycle(trimmed) {
		return time.Time{}, fmt.Errorf(
			"timer %q is a repeating cycle (timeCycle); repeat is not implemented, so use a single duration such as PT10M", expr)
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
