package models

// ProjectModel represents the GORM model for projects.
type ProjectModel struct {
	Base
	OrganizationID UUID              `json:"organization_id,omitzero"`
	Organization   OrganizationModel `json:"organization,omitzero"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitzero"`
}

// TableName overrides the table name for ProjectModel.
func (ProjectModel) TableName() string {
	return "projects"
}
