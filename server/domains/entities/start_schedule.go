package entities

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	// A schedule names its time zone, and a host with no zone database (a
	// minimal container, a stripped package install) would refuse every one of
	// them at deploy. The embedded copy is the fallback the standard library
	// reads when the host has none.
	_ "time/tzdata"
)

// StartScheduleProperty is the start step's property that holds its schedule.
const StartScheduleProperty = "start_schedule"

// How often a schedule starts a run.
const (
	ScheduleEveryInterval = "interval"
	ScheduleEveryDay      = "day"
	ScheduleEveryWeekday  = "weekday"
	ScheduleEveryWeek     = "week"
	ScheduleEveryMonth    = "month"
)

const (
	// MinScheduleIntervalMinutes is the shortest gap an interval schedule may
	// start runs on. Each occurrence is a whole process instance, so the floor
	// is far above a timer's (MinTimerInterval).
	MinScheduleIntervalMinutes = 1
	// MaxScheduleIntervalMinutes is a year. Past it, "every N minutes" is a
	// mistake more often than a plan.
	MaxScheduleIntervalMinutes = 366 * 24 * 60

	scheduleDateLayout = "2006-01-02"
	// scheduleSearchDays bounds the search for the next matching day. Every
	// rule matches at least once a month, so two months is always enough; the
	// bound is what makes that an error instead of an endless loop if a rule is
	// ever added that does not.
	scheduleSearchDays = 62
	// scheduleGapMinutes bounds the search past a wall-clock time that a
	// daylight-saving change skipped. The longest skip on record is a day.
	scheduleGapMinutes = 25 * 60
)

var scheduleWeekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// StartSchedule is when a process starts by itself: the rule a start step's
// "On a schedule" choice is saved as.
//
// It is written by a person choosing from plain options, never as a cron
// expression, so each field is one of those choices and anything else is
// refused at deploy.
type StartSchedule struct {
	// Every is how often: one of the ScheduleEvery values.
	Every string `json:"every"`
	// IntervalMinutes is the gap for ScheduleEveryInterval.
	IntervalMinutes int `json:"interval_minutes,omitzero"`
	// At is the wall-clock time, "15:04", for every rule but the interval.
	At string `json:"at,omitzero"`
	// Days are the days of the week for ScheduleEveryWeek: "mon" … "sun".
	Days []string `json:"days,omitzero"`
	// DayOfMonth is the day for ScheduleEveryMonth. A month too short for it
	// runs on its last day.
	DayOfMonth int `json:"day_of_month,omitzero"`
	// TimeZone is the IANA zone the times are read in.
	TimeZone string `json:"time_zone"`
	// From and Until are optional first and last dates, "2006-01-02", both
	// inclusive and read in TimeZone.
	From  string `json:"from,omitzero"`
	Until string `json:"until,omitzero"`

	location *time.Location
	hour     int
	minute   int
	weekdays []time.Weekday
	from     time.Time
	until    time.Time
}

// HasStartSchedule reports whether a step carries a schedule at all, readable
// or not.
func HasStartSchedule(n *Node) bool {
	if n == nil || n.Properties == nil {
		return false
	}
	raw, present := n.Properties[StartScheduleProperty]
	return present && raw != nil && raw != ""
}

// StartScheduleOf reads a start step's schedule. ok is false when the step has
// none; an error means it has one that cannot be run.
func StartScheduleOf(n *Node) (schedule StartSchedule, ok bool, err error) {
	if !HasStartSchedule(n) {
		return StartSchedule{}, false, nil
	}
	schedule, err = ParseStartSchedule(n.Properties[StartScheduleProperty])
	if err != nil {
		return StartSchedule{}, true, err
	}
	return schedule, true, nil
}

// ParseStartSchedule reads a schedule from a step property, which is a JSON
// object or the same object as text, and checks every field of it.
func ParseStartSchedule(raw any) (StartSchedule, error) {
	var text []byte
	switch v := raw.(type) {
	case string:
		text = []byte(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return StartSchedule{}, fmt.Errorf("the schedule cannot be read: %w", err)
		}
		text = encoded
	}
	var schedule StartSchedule
	if err := json.Unmarshal(text, &schedule); err != nil {
		return StartSchedule{}, errors.New("the schedule is not a JSON object")
	}
	if err := schedule.resolve(); err != nil {
		return StartSchedule{}, err
	}
	return schedule, nil
}

// resolve checks the schedule and works out the values Next reads.
func (s *StartSchedule) resolve() error {
	if strings.TrimSpace(s.TimeZone) == "" {
		return errors.New("the schedule has no time zone")
	}
	// "Local" is whatever the server happens to be set to, which is not a
	// choice anybody made for this process.
	if s.TimeZone == "Local" {
		return errors.New(`the schedule's time zone is "Local"; name a zone such as "Asia/Jakarta" or "UTC"`)
	}
	location, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		return fmt.Errorf("the schedule's time zone %q is not one the server knows", s.TimeZone)
	}
	s.location = location

	if err := s.resolveRule(); err != nil {
		return err
	}
	return s.resolveWindow()
}

func (s *StartSchedule) resolveRule() error {
	switch s.Every {
	case ScheduleEveryInterval:
		if s.IntervalMinutes < MinScheduleIntervalMinutes || s.IntervalMinutes > MaxScheduleIntervalMinutes {
			return fmt.Errorf("the schedule repeats every %d minutes; it must be between %d minute and one year",
				s.IntervalMinutes, MinScheduleIntervalMinutes)
		}
		return nil
	case ScheduleEveryDay, ScheduleEveryWeekday:
		return s.resolveAt()
	case ScheduleEveryWeek:
		if len(s.Days) == 0 {
			return errors.New("the weekly schedule names no day of the week")
		}
		for _, day := range s.Days {
			weekday, known := scheduleWeekdays[strings.ToLower(strings.TrimSpace(day))]
			if !known {
				return fmt.Errorf("the weekly schedule names %q, which is not a day; use mon, tue, wed, thu, fri, sat or sun", day)
			}
			if !slices.Contains(s.weekdays, weekday) {
				s.weekdays = append(s.weekdays, weekday)
			}
		}
		return s.resolveAt()
	case ScheduleEveryMonth:
		if s.DayOfMonth < 1 || s.DayOfMonth > 31 {
			return fmt.Errorf("the monthly schedule runs on day %d; it must be a day from 1 to 31", s.DayOfMonth)
		}
		return s.resolveAt()
	case "":
		return errors.New("the schedule does not say how often it runs")
	default:
		return fmt.Errorf("the schedule runs every %q, which is not one of interval, day, weekday, week or month", s.Every)
	}
}

func (s *StartSchedule) resolveAt() error {
	at, err := time.Parse("15:04", strings.TrimSpace(s.At))
	if err != nil {
		return fmt.Errorf("the schedule's time %q is not a time of day such as 09:00", s.At)
	}
	s.hour, s.minute = at.Hour(), at.Minute()
	return nil
}

func (s *StartSchedule) resolveWindow() error {
	if s.From != "" {
		from, err := time.ParseInLocation(scheduleDateLayout, s.From, s.location)
		if err != nil {
			return fmt.Errorf("the schedule's first date %q is not a date such as 2026-01-31", s.From)
		}
		s.from = from
	}
	if s.Until != "" {
		until, err := time.ParseInLocation(scheduleDateLayout, s.Until, s.location)
		if err != nil {
			return fmt.Errorf("the schedule's last date %q is not a date such as 2026-01-31", s.Until)
		}
		// Inclusive: the whole of the last day counts.
		s.until = until.AddDate(0, 0, 1)
		if !s.from.IsZero() && !s.until.After(s.from) {
			return fmt.Errorf("the schedule's last date %s is before its first date %s", s.Until, s.From)
		}
	}
	return nil
}

// Next returns the first time after `after` that the schedule starts a run.
// ok is false when there is none: the schedule's last date has passed.
//
// It depends on nothing but the schedule and `after`, so asking twice gives the
// same answer and a run is never booked for a time already used.
func (s StartSchedule) Next(after time.Time) (next time.Time, ok bool) {
	if s.location == nil {
		return time.Time{}, false
	}
	if !s.from.IsZero() && after.Before(s.from) {
		// Everything before the first date is skipped; the first date's own
		// midnight is itself a candidate.
		after = s.from.Add(-time.Nanosecond)
	}
	if s.Every == ScheduleEveryInterval {
		next = s.nextInterval(after)
	} else {
		var found bool
		if next, found = s.nextDaily(after); !found {
			return time.Time{}, false
		}
	}
	if !s.until.IsZero() && !next.Before(s.until) {
		return time.Time{}, false
	}
	return next, true
}

// nextInterval counts whole intervals from a fixed anchor, so the times do not
// drift with when each run happened to start, and a server that was down picks
// up on the same beat.
func (s StartSchedule) nextInterval(after time.Time) time.Time {
	anchor := s.from
	if anchor.IsZero() {
		anchor = time.Date(2000, time.January, 1, 0, 0, 0, 0, s.location)
	}
	step := time.Duration(s.IntervalMinutes) * time.Minute
	if after.Before(anchor) {
		return anchor
	}
	elapsed := after.Sub(anchor)
	return anchor.Add(elapsed.Truncate(step) + step)
}

func (s StartSchedule) nextDaily(after time.Time) (time.Time, bool) {
	local := after.In(s.location)
	year, month, day := local.Date()
	for offset := range scheduleSearchDays {
		// Noon, so stepping a day at a time never lands on a different date
		// across a daylight-saving change.
		date := time.Date(year, month, day+offset, 12, 0, 0, 0, s.location)
		if !s.runsOn(date) {
			continue
		}
		y, m, d := date.Date()
		candidate := wallClock(y, m, d, s.hour, s.minute, s.location)
		if candidate.After(after) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// runsOn reports whether the rule picks this date.
func (s StartSchedule) runsOn(date time.Time) bool {
	switch s.Every {
	case ScheduleEveryDay:
		return true
	case ScheduleEveryWeekday:
		return date.Weekday() != time.Saturday && date.Weekday() != time.Sunday
	case ScheduleEveryWeek:
		return slices.Contains(s.weekdays, date.Weekday())
	case ScheduleEveryMonth:
		// Day 0 of next month is the last day of this one.
		lastDay := time.Date(date.Year(), date.Month()+1, 0, 12, 0, 0, 0, date.Location()).Day()
		return date.Day() == min(s.DayOfMonth, lastDay)
	default:
		return false
	}
}

// wallClock is the instant a clock in loc reads the given date and time.
//
// Two days a year a zone with daylight saving has no single answer. A time
// that happens twice (clocks go back) is the first of the two, so the run
// starts once. A time that never happens (clocks go forward) is the next
// minute that does, so the run is late by the size of the skip instead of
// missing. time.Date alone promises neither: it returns "a time that is
// correct in one of the two zones".
func wallClock(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	for skipped := range scheduleGapMinutes {
		if t, ok := earliestReading(year, month, day, hour, minute+skipped, loc); ok {
			return t
		}
	}
	// Unreachable for any zone on record; the nearest instant is still a
	// time to run at.
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

// earliestReading is the first instant at which a clock in loc shows the given
// wall time, and false when it never does.
func earliestReading(year int, month time.Month, day, hour, minute int, loc *time.Location) (time.Time, bool) {
	// The wall time as if it were UTC, which normalises minute overflow too.
	naive := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	var earliest time.Time
	found := false
	// The zone's offset a day either side covers both sides of any change
	// near this time.
	for _, probe := range []time.Time{naive.Add(-24 * time.Hour), naive.Add(24 * time.Hour)} {
		_, offset := probe.In(loc).Zone()
		candidate := naive.Add(-time.Duration(offset) * time.Second).In(loc)
		if !sameWallTime(candidate, naive) {
			continue
		}
		if !found || candidate.Before(earliest) {
			earliest, found = candidate, true
		}
	}
	return earliest, found
}

func sameWallTime(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd && a.Hour() == b.Hour() && a.Minute() == b.Minute()
}
