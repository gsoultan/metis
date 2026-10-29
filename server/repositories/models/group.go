package models

// GroupModel represents the GORM model for user groups.
type GroupModel struct {
	Base
	OrganizationID UUID     `json:"organization_id,omitzero"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitzero"`
	Roles          []string `json:"roles,omitzero"`
}

// TableName overrides the table name for GroupModel.
func (GroupModel) TableName() string {
	return "groups"
}
