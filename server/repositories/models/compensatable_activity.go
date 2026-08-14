package models

import (
	"time"
)

// CompensatableActivityModel is the GORM persistence model for a completed activity
// that is eligible for Saga compensation.
type CompensatableActivityModel struct {
	Base
	InstanceID         UUID           `gorm:"index"            json:"instance_id"`
	NodeID             string         `json:"node_id"`
	CompensationNodeID string         `json:"compensation_node_id"`
	Variables          map[string]any `gorm:"type:text;serializer:json"  json:"variables,omitzero"`
	CompletedAt        time.Time      `json:"completed_at"`
	Compensated        bool           `gorm:"default:false"              json:"compensated"`
}

func (CompensatableActivityModel) TableName() string {
	return "compensatable_activities"
}
