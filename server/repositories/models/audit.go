package models

type AuditModel struct {
	Base
	ProjectID  UUID           `gorm:"index" json:"project_id,omitzero"`
	InstanceID UUID           `gorm:"index" json:"instance_id,omitzero"`
	Type       string         `json:"type"`
	NodeID     string         `json:"node_id,omitzero"`
	NodeName   string         `json:"node_name,omitzero"`
	Message    string         `json:"message"`
	Narrative  string         `json:"narrative,omitzero"`
	Data       map[string]any `gorm:"type:text;serializer:json" json:"data,omitzero"`
	// Seq is the entry's place in the order entries were written. The database
	// assigns it: migration 28 gives the column a sequence as its default. So
	// GORM only reads it, and an insert through GORM takes the default rather
	// than writing a NULL over it. Nil for an entry written before migration 28.
	Seq *int64 `gorm:"->" json:"-"`
}

func (AuditModel) TableName() string {
	return "audit_logs"
}
