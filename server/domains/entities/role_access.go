package entities

// RoleAccess is what one role is required for: every action whose gate
// admits it, read from the gates as they were built.
type RoleAccess struct {
	Role string `json:"role"`

	// Actions is never nil, so a role no gate names encodes as [] rather than
	// null: "required for nothing here" is an answer, not a missing one.
	Actions []RoleAction `json:"actions"`
}
