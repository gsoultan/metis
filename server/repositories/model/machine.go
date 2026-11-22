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
}

func (m *Machine) Schema(t *storm.Table) {
	t.Name("machines")
	t.Col(&m.Name).Size(255)
	t.Col(&m.Address).Size(255)
	t.Col(&m.Role).Size(16)
	t.Col(&m.Version).Size(64)
	t.Index(&m.LastSeenAt).Named("ix_machines_last_seen_at")
}
