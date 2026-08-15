package models

import (
	"time"
)

type IncidentStatus string

const (
	IncidentOpen     IncidentStatus = "open"
	IncidentResolved IncidentStatus = "resolved"
)

type IncidentModel struct {
	Base
	JobID        UUID           `gorm:"index" json:"job_id,omitzero"`
	InstanceID   UUID           `gorm:"index" json:"instance_id,omitzero"`
	DefinitionID UUID           `json:"definition_id,omitzero"`
	NodeID       string         `json:"node_id"`
	Error        string         `json:"error"`
	Status       IncidentStatus `gorm:"index" json:"status"`
	ResolvedAt   *time.Time     `json:"resolved_at,omitzero"`
}

func (IncidentModel) TableName() string {
	return "incidents"
}
