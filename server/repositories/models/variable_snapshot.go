package models

import (
	"time"
)

// VariableSnapshotModel is the GORM persistence model for a process variable snapshot.
type VariableSnapshotModel struct {
	Base
	InstanceID UUID           `json:"instance_id"`
	NodeID     string         `json:"node_id,omitzero"`
	Variables  map[string]any `json:"variables,omitzero"`
	CapturedAt time.Time      `json:"captured_at"`
}

func (VariableSnapshotModel) TableName() string {
	return "variable_snapshots"
}
