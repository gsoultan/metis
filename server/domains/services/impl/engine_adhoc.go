package impl

import (
	"context"
	"slices"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

// checkAdHocCompletion re-evaluates the completion condition of the ad-hoc
// sub-process a finished step belongs to, and lets the process through when it
// is satisfied.
//
// Returns true when it advanced the process, so the caller stops treating the
// finished step as an ordinary node.
//
// What it does with the steps still running inside is BPMN's
// cancelRemainingInstances (2.0.2 §10.3.5, §13.2.5). By default they are
// withdrawn — tokens, tasks, work parked for workers and waiting events —
// before the process moves on: it used to move on and leave them, so a task
// stayed in somebody's inbox for a sub-process that had ended, and its token
// kept the instance from ever finishing. A sub-process that says false waits
// for them instead, and each one finishing asks again.
func (e *Engine) checkAdHocCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node) (bool, error) {
	if node == nil || node.ParentID == "" {
		return false, nil
	}
	parent := def.FindNode(node.ParentID)
	if parent == nil || !parent.IsAdHoc {
		return false, nil
	}
	if len(instance.GetTokensByNode(parent)) == 0 {
		return false, nil
	}
	if parent.CompletionCondition != "" &&
		!logic.GetConditionEvaluatorChain().Evaluate(parent.CompletionCondition, instance.Variables) {
		// More work to do inside; the sub-process keeps waiting.
		return false, e.UpdateInstance(ctx, *instance)
	}

	inside := nodesInside(def, parent)
	if !parent.CancelsRemainingInstances() {
		if holdsAnyToken(instance, inside) {
			// Told to let them finish. The condition is met; what is missing is
			// the steps still running, and the last of them comes back here.
			return false, e.UpdateInstance(ctx, *instance)
		}
	} else if err := e.endAdHocSteps(ctx, instance, def, inside); err != nil {
		return false, err
	}

	// The caller stops at "true", before the place it stops waiting for the
	// finished step's own events — its boundary events, armed when it started.
	// Nothing running holds a token by now, so this step is the only one inside
	// that can still have any; left armed, a message arriving later moved a
	// process that had left the sub-process back into it.
	if err := e.cleanupSubscriptions(ctx, instance, node.ID, def); err != nil {
		return false, err
	}
	return true, e.Proceed(ctx, instance, def, parent.ID)
}

// nodesInside returns everything inside a sub-process, at any depth, in both
// shapes a definition stores it: nested under its container, or beside it
// naming the container as its parent — the two findAdHocChild accepts.
//
// At any depth because a step that is itself a sub-process holds no token
// while it runs: its token moves to the steps inside it, and those are what is
// still running when the sub-process around them finishes.
func nodesInside(def *entities.ProcessDefinition, container *entities.Node) []*entities.Node {
	// By ID, so a node stored in both shapes is taken once and a definition
	// whose parents form a ring is walked once.
	taken := map[string]bool{container.ID: true}
	inside := make([]*entities.Node, 0, len(container.Nodes))
	var take func(node *entities.Node)
	take = func(node *entities.Node) {
		if node == nil || taken[node.ID] {
			return
		}
		taken[node.ID] = true
		inside = append(inside, node)
		for _, child := range node.Nodes {
			take(child)
		}
	}
	for _, child := range container.Nodes {
		take(child)
	}
	// The flat shape lists a child wherever it likes, so a pass that takes
	// something may have made its children takeable: go round until one takes
	// nothing.
	for grew := true; grew; {
		grew = false
		for _, node := range def.Nodes {
			if node != nil && !taken[node.ID] && node.ParentID != "" && taken[node.ParentID] {
				take(node)
				grew = true
			}
		}
	}
	return inside
}

// holdsAnyToken reports whether the instance is still on any of nodes.
func holdsAnyToken(instance *entities.ProcessInstance, nodes []*entities.Node) bool {
	return slices.ContainsFunc(nodes, func(node *entities.Node) bool {
		return len(instance.GetTokensByNode(node)) > 0
	})
}

// endAdHocSteps ends what is still running inside a sub-process that has
// finished: each step's tokens, its count if it repeats, and the events it and
// its boundary events were waiting for; then every task any of them has open,
// and all the work any of them has parked for a worker, in one read each.
//
// It is endActivity for many steps at once, and ends the same four things —
// see there for why the parked work goes too, and for the process a step
// called, which runs on and whose return finds no token waiting for it.
//
// The tasks and the parked work are withdrawn for every step, not only the
// ones holding a token: a step started twice loses both tokens when the first
// of the two finishes, and its second task would otherwise be left behind.
func (e *Engine) endAdHocSteps(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, steps []*entities.Node) error {
	for _, step := range steps {
		if len(instance.GetTokensByNode(step)) == 0 {
			continue
		}
		instance.RemoveTokenByNode(step)
		instance.FinishMultiInstance(step.ID)
		if err := e.cleanupSubscriptions(ctx, instance, step.ID, def); err != nil {
			return err
		}
	}
	if err := e.cancelOpenTasksOn(ctx, instance, steps); err != nil {
		return err
	}
	return e.withdrawExternalTasksOn(ctx, instance, steps)
}
