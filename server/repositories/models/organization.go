package models

// OrganizationModel represents the GORM model for organizations.
type OrganizationModel struct {
	Base
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Projects    []ProjectModel `json:"projects,omitzero"`
}

// TableName overrides the table name for OrganizationModel.
func (OrganizationModel) TableName() string {
	return "organizations"
}
