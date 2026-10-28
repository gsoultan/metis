package models

// MembershipModel represents the GORM model for user-group membership.
type MembershipModel struct {
	UserID  UUID `json:"user_id,omitzero"`
	GroupID UUID `json:"group_id,omitzero"`
}

// TableName overrides the table name for MembershipModel.
func (MembershipModel) TableName() string {
	return "memberships"
}
