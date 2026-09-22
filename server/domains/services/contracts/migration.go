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

// NodeActionKind is what a migration does with the work parked on one node.
//
// A node mapping can only ever answer "where does this work go". Removing an
// approval from a process asks a different question — whether the approval that
// was pending counts as given, as void, or as still owed — and the only way to
// express any of those with a mapping alone is to point the work at some other
// step, which is how somebody else's approval ends up being performed by the
// wrong person.
type NodeActionKind string

const (
	// NodeActionSkip treats the step as performed by nobody: the work on it is
	// cancelled and the instance advances past it as though it had finished.
	//
	// This is "the approval is moot" — the approver's role was eliminated, the
	// step was a formality the business has dropped. The engine's own advance
	// is what runs, so boundary timers are cancelled, multi-instance counts are
	// honoured and gateways are evaluated exactly as they would have been.
	NodeActionSkip NodeActionKind = "skip"

	// NodeActionCancel ends the instance where it stands.
	//
	// This is "the approval was void and so is what it was approving" — the
	// step should never have been there and the work it was part of does not
	// survive it. The instance is not migrated: it will never run again, and
	// its record should show the version it actually ran on.
	NodeActionCancel NodeActionKind = "cancel"
)

// NodeAction is what to do with the work parked on one node, instead of moving
// it.
type NodeAction struct {
	Kind NodeActionKind `json:"kind"`
	// Reason is why, and it is required.
	//
	// A skipped approval with no reason is indistinguishable in the trail from
	// an approval somebody gave, which is the one thing this must never be. The
	// cost of typing it is small and it is the entire value of the record.
	Reason string `json:"reason"`
}

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
	// Actions are the nodes whose work is decided rather than moved, keyed by
	// source node id.
	Actions map[string]NodeAction
	// Actor is who authorised the migration. It is recorded on every instance's
	// trail, because "a step was skipped" is only half an audit answer; the
	// other half is who decided that.
	Actor string
}

// WithAcknowledgedHolds accepts the loss of named compliance-relevant nodes.
func WithAcknowledgedHolds(nodeIDs ...string) MigrationOption {
	return func(o *MigrationOptions) { o.Acknowledged = append(o.Acknowledged, nodeIDs...) }
}

// WithNodeActions decides the work on named nodes instead of moving it.
func WithNodeActions(actions map[string]NodeAction) MigrationOption {
	return func(o *MigrationOptions) {
		if o.Actions == nil {
			o.Actions = map[string]NodeAction{}
		}
		for nodeID, action := range actions {
			o.Actions[nodeID] = action
		}
	}
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
