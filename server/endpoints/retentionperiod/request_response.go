package retentionperiod

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// SetPeriodRequest gives a process a period, or returns it to the
// installation's setting.
type SetPeriodRequest struct {
	// ProjectID and ProcessKey are taken from the address, never the body.
	ProjectID  string `json:"-"`
	ProcessKey string `json:"-"`
	// Days is the period, or nil for the installation's setting. The handler
	// refuses a body that does not say which.
	Days   *int   `json:"days"`
	Reason string `json:"reason"`
}

// SetPeriodResponse is the change recorded.
type SetPeriodResponse struct {
	Period *PeriodView `json:"period,omitzero"`
	Err    error       `json:"err,omitzero"`
}

func (r SetPeriodResponse) Failed() error { return r.Err }

// ListPeriodsRequest names a project, and for a history one of its processes.
type ListPeriodsRequest struct {
	ProjectID  string `json:"-"`
	ProcessKey string `json:"-"`
}

// ListPeriodsResponse is a project's current periods, or one process's
// changes, newest first.
type ListPeriodsResponse struct {
	Periods []PeriodView `json:"periods"`
	Err     error        `json:"err,omitzero"`
}

func (r ListPeriodsResponse) Failed() error { return r.Err }

// PeriodView is a period as the API shows it. Days is null when the process
// follows the installation's setting.
type PeriodView struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	ProcessKey string    `json:"process_key"`
	Days       *int      `json:"days"`
	Reason     string    `json:"reason"`
	SetBy      string    `json:"set_by"`
	SetAt      time.Time `json:"set_at"`
}

// ViewOf maps a period to its view.
func ViewOf(period entities.RetentionPeriod) PeriodView {
	view := PeriodView{
		ID:         period.ID.String(),
		ProcessKey: period.ProcessKey,
		Days:       period.Days,
		Reason:     period.Reason,
		SetBy:      period.SetBy,
		SetAt:      period.CreatedAt,
	}
	if period.Project != nil {
		view.ProjectID = period.Project.ID.String()
	}
	return view
}

func viewsOf(periods []entities.RetentionPeriod) []PeriodView {
	views := make([]PeriodView, 0, len(periods))
	for _, period := range periods {
		views = append(views, ViewOf(period))
	}
	return views
}
