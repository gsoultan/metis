package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Machine is one process of this installation, as it last said it was. Its
// id is the process's own, so a heartbeat writes the same row each time.
type Machine struct {
	storm.Model

	Name       string
	Address    string
	Role       string
	Version    string
	StartedAt  time.Time
	LastSeenAt time.Time
	// RequestedRole is the role an administrator asked this process to take
	// from the Servers page, or empty. The process reads it on its heartbeat
	// and starts again in that role.
	RequestedRole string
	// RoleFixed is whether the operator set the process's role where the
	// Servers page cannot change it: METIS_ROLE, or no config.yaml to keep it.
	RoleFixed bool
}

func (m *Machine) Schema(t *storm.Table) {
	t.Name("machines")
	t.Col(&m.Name).Size(255)
	t.Col(&m.Address).Size(255)
	t.Col(&m.Role).Size(16)
	t.Col(&m.Version).Size(64)
	// Defaults, so the heartbeat of a machine still on the release before
	// these columns, during an upgrade, is not refused.
	t.Col(&m.RequestedRole).Size(16).Default(blankText)
	t.Col(&m.RoleFixed).Default(no)
	t.Index(&m.LastSeenAt).Named("ix_machines_last_seen_at")
}
