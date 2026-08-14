package models

type FormModel struct {
	Base
	ProjectID UUID           `gorm:"index" json:"project_id,omitzero"`
	Key       string         `gorm:"size:255;index" json:"key"`
	Name      string         `json:"name"`
	Schema    map[string]any `gorm:"type:text;serializer:json" json:"schema,omitzero"`
}

func (FormModel) TableName() string {
	return "forms"
}
