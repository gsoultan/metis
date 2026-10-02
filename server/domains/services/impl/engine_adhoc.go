package impl

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
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
	if !instance.WaitsFor(parent, "") {
		return false, nil
	}
	if parent.CompletionCondition != "" &&
		!logic.GetConditionEvaluatorChain().Evaluate(parent.CompletionCondition, instance.Variables) {
		// More work to do inside; the sub-process keeps waiting.
		return false, e.UpdateInstance(ctx, *instance)
	}

	inside := nodesInside(def, parent)
	if parent.CancelsRemainingInstances() {
		if err := e.endAdHocSteps(ctx, instance, def, inside); err != nil {
			return false, err
		}
		return true, e.Proceed(ctx, instance, def, parent.ID)
	}

	if holdsAnyToken(instance, inside) {
		// Told to let them finish. The condition is met; what is missing is
		// the steps still running, and the last of them comes back here.
		return false, e.UpdateInstance(ctx, *instance)
	}
	// The caller stops at "true", before the place it stops waiting for the
	// finished step's own events — its boundary events, armed when it started.
	// Every other step inside finished the ordinary way and gave its events up
	// there, so this one's are the only ones left.
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
// finished: every token and repeat count on a step inside it, every event any
// step or its boundary events were waiting for, every task any of them has
// open, and all the work any of them has parked for a worker — one read each
// of the instance's events, tasks and parked work, however many steps there
// are.
//
// It is endActivity for many steps at once, and ends the same things — see
// there for why the parked work goes too, and for the process a step called,
// which runs on and whose return finds no token waiting for it.
//
// None of it is limited to the steps holding a token. A step started twice
// loses both tokens when the first of the two finishes, and its second task
// would otherwise be left behind. A step that is itself a sub-process holds no
// token while it runs, and its boundary events were armed when it was entered.
// And the step whose finishing ended the sub-process gave its token up before
// this was asked, with its boundary events still armed.
func (e *Engine) endAdHocSteps(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, steps []*entities.Node) error {
	for _, step := range steps {
		instance.RemoveTokenByNode(step)
		instance.FinishMultiInstance(step.ID)
	}
	if err := e.stopWaitingOn(ctx, instance, def, steps); err != nil {
		return err
	}
	if err := e.cancelOpenTasksOn(ctx, instance, steps); err != nil {
		return err
	}
	return e.withdrawExternalTasksOn(ctx, instance, steps)
}

// stopWaitingOn deletes the event subscriptions the instance holds for any of
// nodes or for a boundary event attached to one — what cleanupSubscriptions
// does for one node, for many, reading the instance's subscriptions once.
//
// A subscription that outlives its step is a message or signal that can still
// be delivered to it: delivery asks for no token, so it followed the event's
// outgoing flows inside a sub-process the instance had left.
func (e *Engine) stopWaitingOn(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodes []*entities.Node) error {
	ours := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		ours[node.ID] = true
		for _, boundary := range def.GetBoundaryEvents(node.ID) {
			ours[boundary.ID] = true
		}
	}
	if len(ours) == 0 {
		return nil
	}

	waiting, err := e.repo.Subscription().ListByInstance(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("list event subscriptions for instance %s: %w", instance.ID, err)
	}
	for _, sub := range waiting {
		if !ours[sub.NodeID] {
			continue
		}
		if err := e.repo.Subscription().Delete(ctx, uuid.UUID(sub.ID)); err != nil {
			return fmt.Errorf("delete subscription for node %s: %w", sub.NodeID, err)
		}
	}
	return nil
}
