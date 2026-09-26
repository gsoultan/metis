package entities

import (
	"time"

	"github.com/google/uuid"
)

// PlatformUser is an account that administers Metis.
//
// Not the same population as a WorkflowUser. These are the handful of people
// who configure environments, author process models and manage accounts;
// participants are everybody in the business a process might name.
type PlatformUser struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	FullName    string    `json:"full_name,omitzero"`
	DisplayName string    `json:"display_name,omitzero"`
	Email       string    `json:"email,omitzero"`

	// Roles are what this account may do. Names rather than ids, because that
	// is what the authorization checks compare against and what a person reads.
	Roles []string `json:"roles,omitzero"`

	CreatedAt time.Time `json:"created_at,omitzero"`
}

// PlatformRole is a named set of things an administrator may do.
type PlatformRole struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitzero"`
	// Permissions are the endpoint names this role admits. Held as data so what
	// a role grants is answerable by reading a row rather than by reading the
	// code that enforces it.
	Permissions []string `json:"permissions,omitzero"`
	// BuiltIn roles ship with the installation. They can be granted and revoked
	// but not deleted: an installation with no ADMIN role is one nobody can
	// administer.
	BuiltIn bool `json:"built_in"`
}

// BuiltInPlatformRoles are the roles every installation has.
//
// RoleUser is deliberately absent. It used to mean "participates in processes —
// the task inbox", which is now what being a WorkflowUser *is*: a participant
// needs no platform role to have an inbox, and granting one would put them back
// in the population this split took them out of.
func BuiltInPlatformRoles() []PlatformRole {
	return []PlatformRole{
		{
			Name:        RoleAdmin,
			Description: "Administers the platform: accounts, organizations, projects, environments and connectors.",
		},
		{
			Name:        RoleDesigner,
			Description: "Authors and deploys process and decision models.",
		},
		{
			Name:        RoleOperator,
			Description: "Runs the system day to day: resolves incidents, starts ad hoc tasks, broadcasts signals.",
		},
		{
			Name:        RoleQueryAuthor,
			Description: "Deploys process models that look things up in a connected database.",
		},
	}
}
