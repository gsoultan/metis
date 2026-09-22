package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MigrationService moves running instances from one version of a process onto
// another.
//
// The supported way to change version is to promote a new one and let the old
// one drain: an instance that finishes on the graph it started with cannot be
// broken by an edit. This is for the case drain cannot serve — work already in
// flight on a version that must not continue.
type MigrationService interface {
	// PlanInstanceMigration reports what MigrateInstances would do, and writes
	// nothing. Every refusal MigrateInstances would make appears here, so a
	// preview cannot approve something the apply rejects.
	PlanInstanceMigration(ctx context.Context, sourceDefID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...MigrationOption) (entities.MigrationPlan, error)

	MigrateInstances(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...MigrationOption) error
}

// MigrationOption adjusts one migration.
//
// Variadic rather than a wider signature so that the ordinary call — move this
// work onto that version — stays the short one. Everything here is about a
// migration that needs a human to take responsibility for something, which is
// not the common case and should not be in the common case's way.
type MigrationOption func(*MigrationOptions)

// MigrationOptions is what the options add up to.
type MigrationOptions struct {
	// Acknowledged are the compliance-relevant nodes whose loss the caller has
	// accepted. A hold that is not named here is a refusal.
	//
	// Node ids rather than a single "yes, I am sure" flag on purpose: a blanket
	// override is a button people learn to press, and it would carry over
	// silently to whatever the next version of the plan happens to hold. Naming
	// each one means the acknowledgement stops applying the moment the plan
	// changes under it.
	Acknowledged []string
	// Actor is who authorised the migration. It is recorded on every instance's
	// trail, because "a step was skipped" is only half an audit answer; the
	// other half is who decided that.
	Actor string
}

// WithAcknowledgedHolds accepts the loss of named compliance-relevant nodes.
func WithAcknowledgedHolds(nodeIDs ...string) MigrationOption {
	return func(o *MigrationOptions) { o.Acknowledged = append(o.Acknowledged, nodeIDs...) }
}

// WithActor records who authorised the migration.
func WithActor(actor string) MigrationOption {
	return func(o *MigrationOptions) { o.Actor = actor }
}

// ApplyMigrationOptions folds a list of options into one value.
func ApplyMigrationOptions(opts []MigrationOption) MigrationOptions {
	var resolved MigrationOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return resolved
}
