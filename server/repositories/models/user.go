package models

import "time"

// UserModel represents the GORM model for users.
type UserModel struct {
	Base
	Username     string `gorm:"size:255;uniqueIndex" json:"username"`
	PasswordHash string `json:"-"`

	// TokensValidFrom is when this account's credentials last changed. Tokens
	// issued before it are refused.
	//
	// Without it a password change ended nothing: the old password stopped
	// working while every token minted with it kept full access for the rest of
	// its 24-hour life. Somebody changing their password because they think
	// they have been compromised is doing it to end the attacker's access, and
	// that is the one thing it did not do.
	//
	// Nullable because existing rows predate it, and a zero time there would
	// invalidate every session on the deployment that adds the column.
	TokensValidFrom *time.Time          `json:"-"`
	FullName        string              `json:"full_name"`
	DisplayName     string              `json:"display_name"`
	Organization    string              `json:"organization"`
	Email           string              `json:"email"`
	Roles           []string            `gorm:"type:text;serializer:json" json:"roles,omitzero"`
	Organizations   []OrganizationModel `gorm:"many2many:user_organizations" json:"organizations,omitzero"`
	Projects        []ProjectModel      `gorm:"many2many:user_projects" json:"projects,omitzero"`
}

// TableName overrides the table name for UserModel.
func (UserModel) TableName() string {
	return "users"
}
