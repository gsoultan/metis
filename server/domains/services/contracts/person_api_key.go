package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PersonAPIKeyService lets a signed-in person make, list and revoke the keys
// their own systems call the People task API with.
type PersonAPIKeyService interface {
	// CreatePersonAPIKey makes a named key and returns it with the key itself,
	// shown this once and never again.
	CreatePersonAPIKey(ctx context.Context, person entities.Person, name string) (entities.PersonAPIKey, string, error)
	ListPersonAPIKeys(ctx context.Context, person entities.Person) ([]entities.PersonAPIKey, error)
	RevokePersonAPIKey(ctx context.Context, person entities.Person, id uuid.UUID) error
}
