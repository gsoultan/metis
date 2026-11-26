package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// MachineJoin is a machine being added with a one-time code. Only the code's
// SHA-256 is kept. Sealed is this installation's settings sealed to Key, the
// machine's own public key, and is emptied once the machine collects it.
type MachineJoin struct {
	storm.Model

	CodeHash    string
	Role        string
	CreatedBy   string
	ExpiresAt   time.Time
	Name        string
	MachineKey  string
	Words       string
	RequestedAt *time.Time
	AcceptedBy  string
	AcceptedAt  *time.Time
	Sealed      string
	CollectedAt *time.Time
}

func (j *MachineJoin) Schema(t *storm.Table) {
	t.Name("machine_joins")
	t.Col(&j.CodeHash).Size(64)
	t.Col(&j.Role).Size(16)
	t.Col(&j.CreatedBy).Size(255)
	t.Col(&j.Name).Size(255)
	t.Col(&j.MachineKey).Size(64)
	t.Col(&j.Words).Size(64)
	t.Col(&j.AcceptedBy).Size(255)
	t.Col(&j.Sealed).Size(16384)
	t.Index(&j.CodeHash).Unique().Named("ux_machine_joins_code_hash")
	t.Index(&j.ExpiresAt).Named("ix_machine_joins_expires_at")
}
