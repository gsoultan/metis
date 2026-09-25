package entities

// Profile is the part of an account its owner may change: how they are named
// and where mail reaches them.
//
// Roles, organizations, the username and the password have no field here, so
// a request carrying them has nowhere to put them. Roles are global and decide
// what somebody may do in every organization they belong to; the account's
// owner is the one person who must not be able to set them.
type Profile struct {
	FullName    string `json:"full_name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}
