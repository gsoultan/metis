package models

import (
	"time"
)

// CompensatableActivityModel is the GORM persistence model for a completed activity
// that is eligible for Saga compensation.
type CompensatableActivityModel struct {
	Base
	InstanceID         UUID           `json:"instance_id"`
	NodeID             string         `json:"node_id"`
	CompensationNodeID string         `json:"compensation_node_id"`
	Variables          map[string]any `json:"variables,omitzero"`
	CompletedAt        time.Time      `json:"completed_at"`
	Compensated        bool           `json:"compensated"`
}

func (CompensatableActivityModel) TableName() string {
	return "compensatable_activities"
}
