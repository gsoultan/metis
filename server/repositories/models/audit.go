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
}

func (AuditModel) TableName() string {
	return "audit_logs"
}
