package entities

import (
	"time"

	"github.com/google/uuid"
)

// MaxRetentionPeriodDays is the longest retention period a process can be
// given, in days: the bound the installation's own setting has, so a typo of
// extra digits is refused rather than kept for ever.
const MaxRetentionPeriodDays = 36_500

// RetentionPeriod is how long one process's finished instances are kept, as
// an administrator last set it: for every version of the process, completed
// and cancelled instances stay for Days days after they last changed, and
// the retention sweep then removes them as it removes any other.
//
// Days is nil when the process follows the installation's own setting again.
// Each change is kept, so the periods a process has had, who set each and
// why, can be read back.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type RetentionPeriod struct {
	ID         uuid.UUID
	CreatedAt  time.Time
	Project    *Project
	ProcessKey string
	Days       *int

	Reason  string
	SetBy   string
	SetByID uuid.UUID
}
