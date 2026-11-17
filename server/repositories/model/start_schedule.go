package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// StartSchedule is how far one process's scheduled starts have got: one row
// for each process key that has, or had, a start step with a schedule.
//
// The schedule itself is not here. It is the live version's, read from the
// definition each time, so a version that is no longer live cannot start a
// run and there is no copy to fall out of step with a deploy. What has to be
// kept is the one thing the definition and the clock cannot say: which
// scheduled times have already been dealt with.
type StartSchedule struct {
	storm.Model

	Project Project

	// ProcessKey with Project is the row's identity: one per process.
	ProcessKey string

	// CheckedUntil is the watermark. Every scheduled time up to and including
	// it has been dealt with, so a run is started only for a time after it.
	// Moved in the transaction that starts the run, which is what makes a
	// scheduled time start one run however many replicas see it due.
	CheckedUntil time.Time

	// NextRunAt is when the row next needs looking at: the next scheduled
	// time, or the moment another version is arranged to take over. Nil when
	// the live version has no schedule and no cutover is arranged.
	NextRunAt *time.Time

	// Failures counts consecutive attempts that could not start the run, and
	// LastError is the latest reason. Both are cleared by a start that works.
	Failures  int
	LastError *string
}

func (s *StartSchedule) Schema(t *storm.Table) {
	t.Name("start_schedules")
	t.Col(&s.ProcessKey).Size(191)
	t.Col(&s.Failures).Default(zero)
	t.Unique(&s.ProcessKey, &s.Project)
	// The claim's: the rows whose time has come, oldest first.
	t.Index(&s.NextRunAt).Named("ix_start_schedules_next_run_at")
}
