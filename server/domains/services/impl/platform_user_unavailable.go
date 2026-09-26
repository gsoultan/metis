package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// platformUnavailableReason is separate from the participant directory's
// because the consequence is different: without this page an administrator can
// still sign in and run the installation, they just cannot manage accounts from
// the UI.
const platformUnavailableReason = "managing platform accounts needs PostgreSQL; this installation is on another database engine"

type unavailablePlatformUsers struct{}

// NewUnavailablePlatformUserService stands in when there is no storm
// connection, for the same reason NewUnavailableWorkflowUserService does: a nil
// service panics on the first request, and an empty list looks like an
// installation with no administrators — which is the one thing that cannot be
// true of an installation somebody is signed in to.
func NewUnavailablePlatformUserService() servicecontracts.PlatformUserService {
	return unavailablePlatformUsers{}
}

func (unavailablePlatformUsers) ListPlatformUsers(context.Context) ([]entities.PlatformUser, error) {
	return nil, apierr.Invalidf("%s", platformUnavailableReason)
}

func (unavailablePlatformUsers) CreatePlatformUser(context.Context, entities.PlatformUser, string) (uuid.UUID, error) {
	return uuid.Nil, apierr.Invalidf("%s", platformUnavailableReason)
}

func (unavailablePlatformUsers) UpdatePlatformUser(context.Context, entities.PlatformUser) error {
	return apierr.Invalidf("%s", platformUnavailableReason)
}

func (unavailablePlatformUsers) DeletePlatformUser(context.Context, uuid.UUID) error {
	return apierr.Invalidf("%s", platformUnavailableReason)
}

func (unavailablePlatformUsers) SetPlatformRoles(context.Context, uuid.UUID, []string) error {
	return apierr.Invalidf("%s", platformUnavailableReason)
}

// ListPlatformRoles answers with the built-in roles rather than a refusal.
//
// The role list is a fact about the software, not about the database: it is the
// same three names on every installation. Returning them lets the UI describe
// what a role means even where accounts cannot be edited.
func (unavailablePlatformUsers) ListPlatformRoles(context.Context) ([]entities.PlatformRole, error) {
	roles := entities.BuiltInPlatformRoles()
	for i := range roles {
		roles[i].BuiltIn = true
	}
	return roles, nil
}
