package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// PersonDevice is a phone or browser a person registered for push.
//
// Rows are deleted rather than marked: a device that is removed, or that
// Firebase says is gone, has nothing worth keeping, and finding 17 of the
// stability review is about tables that only ever grow. A device belongs to
// one person: registering a token another person had moves it, because the
// phone changed hands or somebody else signed in on it.
//
// ProjectID is the participant's project, kept so an administrator's read is
// scoped to their project without a join. A participant never moves project.
type PersonDevice struct {
	storm.Model

	Project     Project
	Participant WorkflowUser

	Token      string
	TokenHash  string
	Platform   string
	Label      string
	LastSentAt *time.Time
}

func (d *PersonDevice) Schema(t *storm.Table) {
	t.Name("person_devices")
	// An FCM registration token is about 160 characters today; the room is
	// for whatever Firebase issues next.
	t.Col(&d.Token).Size(4096)
	t.Col(&d.TokenHash).Size(64)
	t.Col(&d.Platform).Size(16)
	t.Col(&d.Label).Size(100)
	// One row per device, whoever holds it now.
	t.Index(&d.TokenHash).Unique().Named("ux_person_devices_token_hash")
}
