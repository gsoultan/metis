package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ActivityFinisher ends a step an instance is waiting at, whole, and moves the
// instance on from it: every run of a step that repeats, not the one a
// completion would name.
//
// It is an interface of its own rather than a method of EngineRunner because
// only what waives a step uses it. A caller holding an engine asks whether it
// is one; an engine that is not cannot end a repeating step whole, and the
// caller says so rather than guessing.
type ActivityFinisher interface {
	// FinishActivity ends the step nodeID of def on instance — its tokens,
	// the count of a repeating approval, the tasks it has open, which are
	// withdrawn and announced — and follows what comes after it once. The
	// caller holds the instance's lock; def is the graph the instance runs.
	FinishActivity(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error
}
