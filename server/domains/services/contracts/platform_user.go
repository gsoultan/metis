package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PlatformUserService manages the accounts that administer Metis.
//
// Every method here is administrative. The service does not authorize —
// the transport does, and wires all of it behind the admin gate — but it does
// refuse the changes that would leave the installation unadministrable, because
// that is a rule about the data and not about who asked.
type PlatformUserService interface {
	ListPlatformUsers(ctx context.Context) ([]entities.PlatformUser, error)
	CreatePlatformUser(ctx context.Context, account entities.PlatformUser, password string) (uuid.UUID, error)
	UpdatePlatformUser(ctx context.Context, account entities.PlatformUser) error
	DeletePlatformUser(ctx context.Context, id uuid.UUID) error

	// SetPlatformRoles replaces an account's grants.
	SetPlatformRoles(ctx context.Context, id uuid.UUID, roles []string) error

	ListPlatformRoles(ctx context.Context) ([]entities.PlatformRole, error)
}
