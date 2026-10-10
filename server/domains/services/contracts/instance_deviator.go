package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// InstanceDeviator deals with one running instance where it stands, outside
// what its process says: it waives the step the instance waits at, cancels
// the instance, or holds it for somebody to decide.
type InstanceDeviator interface {
	// DeviateInstance answers the plan for a command, and applies it when the
	// command is not a dry run. A plan that refuses is an answer, not an
	// error. The caller must be a signed-in administrator of the organization
	// the request is for; an instance of another organization is not found.
	DeviateInstance(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error)
}
