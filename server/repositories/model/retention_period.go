package model

import "github.com/gsoultan/storm"

// RetentionPeriod is one change to how long a process's finished instances
// are kept: an administrator said, for every version of one process of a
// project, how many days its completed and cancelled instances stay before
// the retention sweep removes them, or that the installation's own setting
// applies again.
//
// Rows are never changed or removed. The period a process has is the one its
// newest row says, and the rows before it are the history of who changed it,
// when and why: there is no audit trail for a project, only for an instance.
type RetentionPeriod struct {
	storm.Model

	Project Project
	// ProcessKey names the process, every version of it, as a definition's
	// key does.
	ProcessKey string
	// Days is the period. Nil returns the process to the installation's
	// METIS_RETENTION_FINISHED_INSTANCES_DAYS, which may be unset.
	Days *int32

	Reason  string
	SetBy   string
	SetByID storm.UUID
}

func (p *RetentionPeriod) Schema(t *storm.Table) {
	t.Name("retention_periods")
	t.Col(&p.ProcessKey).Size(255)
	t.Col(&p.SetBy).Size(255)
	// A process's newest row, which the sweep reads for each instance it
	// considers, and the history of one process, newest first.
	t.Index(&p.Project, &p.ProcessKey, &p.CreatedAt, &p.ID).Named("ix_retention_periods_process")
}
