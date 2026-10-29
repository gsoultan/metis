package entities

import (
	"time"

	"github.com/google/uuid"
)

// InstallationSetting is a server setting as an administrator last set it on
// the Settings page, or one change in its history.
//
// Value is nil when the setting was cleared, which returns it to the
// environment's value or the default. Secret says Value must never leave the
// server: it is sealed at rest, and a reader is told only that one is set.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type InstallationSetting struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Name      string
	Value     *string
	Secret    bool

	Reason  string
	SetBy   string
	SetByID uuid.UUID
}
