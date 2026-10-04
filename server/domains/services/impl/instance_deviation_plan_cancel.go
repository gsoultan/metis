package impl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// planCancel asks what only a cancel needs: that nothing else is waiting on
// the instance or running under it, and — when the command names no step —
// that the instance waits nowhere.
//
// A called instance that waits somewhere is part of a larger process, and is
// refused. One that waits nowhere will never end, so it will never resume its
// caller, and its caller cannot be cancelled while it runs: it may be closed
// alone, with a warning naming the caller (rulings addendum §13).
func (s *instanceDeviationService) planCancel(ctx context.Context, p *planning) error {
	waitsSomewhere := len(p.instance.Tokens) > 0
	if caller := p.instance.ParentInstance; caller != nil && waitsSomewhere {
		p.refuse("This instance was started by another process (instance %s); cancel that one, or hold this one.", caller.ID)
	}
	running, err := s.calledAndRunning(ctx, p.instance.ID, "")
	if err != nil {
		return err
	}
	if p.plan.CalledInstances = running; len(running) > 0 {
		p.refuse("This instance is waiting on %d process(es) it started (%s); cancel or finish those first.",
			len(running), listed(idsAsText(running)))
	}
	switch {
	case waitsSomewhere && p.command.NodeID == "":
		// The record of a cancel names where the instance stood whenever it
		// stood somewhere.
		p.refuse("This instance is waiting at %s; say which of those steps it is to be ended at.", p.whereItWaits())
	case !waitsSomewhere && p.instance.Status == entities.ProcessActive:
		if err := s.warnNothingLeft(ctx, p); err != nil {
			return err
		}
	}
	p.warnWhoLosesWork()
	return nil
}

// whereItWaits names the steps the instance holds a token on: each once, by
// name, in order.
func (p *planning) whereItWaits() string {
	names := map[string]struct{}{}
	for _, token := range p.instance.Tokens {
		if token.Node == nil {
			continue
		}
		name := token.Node.ID
		if node := p.def.FindNode(token.Node.ID); node != nil && node.Name != "" {
			name = node.Name
		}
		names["“"+name+"”"] = struct{}{}
	}
	return listed(sortedKeys(names))
}

// warnNothingLeft says what closing an instance that waits nowhere closes:
// the instance, any task still open under it, and nothing of the process
// that started it.
func (s *instanceDeviationService) warnNothingLeft(ctx context.Context, p *planning) error {
	p.warn("This instance has nothing left to do: it is not waiting at any step, and nothing will move it on. Cancelling it closes it.")
	for _, task := range p.open {
		p.warn("“%s” is still open though the instance is not waiting there; it will be withdrawn.", taskName(task))
	}
	caller := p.instance.ParentInstance
	if caller == nil {
		return nil
	}
	// Said only of a caller that is still running: one that has ended is
	// waiting for nothing, and cannot be cancelled or held next.
	parent, err := s.engine.GetInstance(ctx, caller.ID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the instance that started instance %s: %w", p.instance.ID, err)
	}
	if parent.Status == entities.ProcessActive {
		p.warn("This instance was started by another process (instance %s), which is still waiting for it and is not resumed by this; "+
			"cancel or hold that one next.", caller.ID)
	}
	return nil
}

// calledAndRunning is the processes an instance started that are still
// running — those one step started, when a step is named — by id, in order.
func (s *instanceDeviationService) calledAndRunning(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]uuid.UUID, error) {
	called, err := s.engine.ListSubProcesses(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the processes instance %s started: %w", instanceID, err)
	}
	var running []uuid.UUID
	for _, child := range called {
		if child.Status != entities.ProcessActive {
			continue
		}
		if nodeID != "" && (child.ParentNode == nil || child.ParentNode.ID != nodeID) {
			continue
		}
		running = append(running, child.ID)
	}
	slices.SortFunc(running, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return running, nil
}
