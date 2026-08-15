package models

// UserModel represents the GORM model for users.
type UserModel struct {
	Base
	Username      string              `gorm:"size:255;uniqueIndex" json:"username"`
	PasswordHash  string              `json:"-"`
	FullName      string              `json:"full_name"`
	DisplayName   string              `json:"display_name"`
	Organization  string              `json:"organization"`
	Email         string              `json:"email"`
	Roles         []string            `gorm:"type:text;serializer:json" json:"roles,omitzero"`
	Organizations []OrganizationModel `gorm:"many2many:user_organizations" json:"organizations,omitzero"`
	Projects      []ProjectModel      `gorm:"many2many:user_projects" json:"projects,omitzero"`
}

// TableName overrides the table name for UserModel.
func (UserModel) TableName() string {
	return "users"
}
