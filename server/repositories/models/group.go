package models

// GroupModel represents the GORM model for user groups.
type GroupModel struct {
	Base
	OrganizationID UUID     `gorm:"index;uniqueIndex:idx_group_name_org" json:"organization_id,omitzero"`
	Name           string   `gorm:"size:255;uniqueIndex:idx_group_name_org" json:"name"`
	Description    string   `json:"description,omitzero"`
	Roles          []string `gorm:"type:text;serializer:json" json:"roles,omitzero"`
}

// TableName overrides the table name for GroupModel.
func (GroupModel) TableName() string {
	return "groups"
}
