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
	TokensValidFrom *time.Time `json:"-"`
	FullName        string     `json:"full_name"`
	DisplayName     string     `json:"display_name"`
	Organization    string     `json:"organization"`
	Email           string     `json:"email"`
	Roles           []string   `gorm:"type:text;serializer:json" json:"roles,omitzero"`

	// IdentityIssuer and IdentitySubject link the account to the identity
	// provider that signs it in, and are nil for a local account. Unique over
	// the live rows, under the name the storm model gives the index, so the two
	// layers describe one index rather than two. Migration 27 adds both to an
	// installation that predates them.
	IdentityIssuer  *string `gorm:"uniqueIndex:uq_users_identity_issuer_identity_subject,where:deleted_at IS NULL" json:"-"`
	IdentitySubject *string `gorm:"uniqueIndex:uq_users_identity_issuer_identity_subject,where:deleted_at IS NULL" json:"-"`

	// Loaded by the repository, not by the ORM. These were a GORM many-to-many,
	// which derived the join columns from the Go type names — user_model_id,
	// organization_model_id — and then kept creating them alongside the ones
	// the schema actually wanted, so a row could satisfy one pair of foreign
	// keys and violate the other.
	//
	// The join tables belong to the storm model now (UserOrganization,
	// UserProject) and these are plain fields the repository fills.
	Organizations []OrganizationModel `gorm:"-" json:"organizations,omitzero"`
	Projects      []ProjectModel      `gorm:"-" json:"projects,omitzero"`
}

// TableName overrides the table name for UserModel.
func (UserModel) TableName() string {
	return "users"
}
