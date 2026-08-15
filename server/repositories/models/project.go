package models

// ProjectModel represents the GORM model for projects.
type ProjectModel struct {
	Base
	OrganizationID UUID              `gorm:"index;uniqueIndex:idx_project_name_org" json:"organization_id,omitzero"`
	Organization   OrganizationModel `gorm:"foreignKey:OrganizationID" json:"organization,omitzero"`
	Name           string            `gorm:"size:255;uniqueIndex:idx_project_name_org" json:"name"`
	Description    string            `json:"description,omitzero"`
}

// TableName overrides the table name for ProjectModel.
func (ProjectModel) TableName() string {
	return "projects"
}
