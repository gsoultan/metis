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
	"github.com/gsoultan/metis/server/repositories/models"
)

// planCancel asks what only a cancel needs: that nothing else is waiting on
// the instance or running under it, and — when the command names no step —
// that the instance waits nowhere.
//
// A cancel ends the whole instance, whichever step it names: the plan lists
// every task open on it, warns of each that would be taken from somebody,
// says which are open on a step the instance is not waiting at, and says how
// much work parked for outside workers it would withdraw and how many open
// incidents it would close.
//
// A called instance may be cancelled alone, wherever it stands. Its caller
// cannot be cancelled while it has not ended, so it is the one that goes
// first: when the caller has not ended either, the plan warns that the caller
// is still waiting for it and is not resumed (warnOfTheCaller). The cancel
// ends this instance and moves nobody else.
func (s *instanceDeviationService) planCancel(ctx context.Context, p *planning) error {
	waitsSomewhere := len(p.instance.Tokens) > 0
	running := p.instance.Status == entities.ProcessActive
	called, err := s.calledAndNotEnded(ctx, p.instance.ID, "")
	if err != nil {
		return err
	}
	if p.plan.CalledInstances = called; len(called) > 0 {
		p.refuse("This instance is waiting on %d process(es) it started (%s); cancel or finish those first.",
			len(called), namesShown(idsAsText(called)))
	}
	if waitsSomewhere && p.command.NodeID == "" {
		// The record of a cancel names where the instance stood whenever it
		// stood somewhere.
		p.refuse("This instance is waiting at %s; say which of those steps it is to be ended at.", p.whereItWaits())
	}
	if running && !waitsSomewhere {
		// Only what was looked at is said: where the instance waits. Whether
		// a timer or a message could still move it was not.
		p.warn("This instance is not waiting at any step. Cancelling it closes it.")
	}
	if running {
		p.warnOfWorkNobodyWaitsFor()
	}
	if running {
		if err := s.warnOfTheCaller(ctx, p); err != nil {
			return err
		}
	}
	p.warnWhoLosesWork()
	if !running {
		return nil
	}
	if err := s.warnOfParkedWorkToWithdraw(ctx, p); err != nil {
		return err
	}
	return s.warnOfIncidentsToClose(ctx, p)
}

// warnOfParkedWorkToWithdraw says how much work parked for outside workers a
// cancel would take back: every piece the instance has parked, on whichever
// step. A worker is not somebody the plan can name, so the pieces are counted:
// an administrator is shown everything the act would take before it is taken.
func (s *instanceDeviationService) warnOfParkedWorkToWithdraw(ctx context.Context, p *planning) error {
	parked, err := s.repo.ExternalTask().ListByProcessInstance(ctx, p.instance.ID)
	if err != nil {
		return fmt.Errorf("reading the work instance %s has parked for workers: %w", p.instance.ID, err)
	}
	if len(parked) > 0 {
		p.warn("%d piece(s) of work parked for outside workers will be withdrawn.", len(parked))
	}
	return nil
}

// warnOfIncidentsToClose says how many incidents a cancel would close: every
// one the instance has open, on whichever step. An incident asks somebody to
// decide about an instance, and on a cancelled one there is nothing left to
// decide; the cancel closes them, and its record names them.
func (s *instanceDeviationService) warnOfIncidentsToClose(ctx context.Context, p *planning) error {
	incidents, err := s.repo.Incident().ListByInstance(ctx, p.instance.ID)
	if err != nil {
		return fmt.Errorf("reading the incidents on instance %s: %w", p.instance.ID, err)
	}
	open := 0
	for _, incident := range incidents {
		if incident.Status == models.IncidentOpen {
			open++
		}
	}
	if open > 0 {
		p.warn("%d open incident(s) on this instance will be closed.", open)
	}
	return nil
}

// whereItWaits names the steps the instance holds a token on: each once, by
// name, in order.
func (p *planning) whereItWaits() string {
	names := map[string]struct{}{}
	for _, token := range p.instance.Tokens {
		if token.Node != nil {
			names[p.stepName(token.Node.ID)] = struct{}{}
		}
	}
	return quotedNamesShown(sortedKeys(names))
}

// warnOfWorkNobodyWaitsFor says which of the tasks a cancel lists are open on
// a step the instance holds no token on — work the process is no longer
// waiting for, wherever else the instance waits. A cancel withdraws it with
// the rest. The tasks the plan lists are spoken of one by one, and the rest
// counted.
func (p *planning) warnOfWorkNobodyWaitsFor() {
	waitedAt := make(map[string]struct{}, len(p.instance.Tokens))
	for _, token := range p.instance.Tokens {
		if token.Node != nil {
			waitedAt[token.Node.ID] = struct{}{}
		}
	}
	nobodyWaits := func(task models.TaskModel) bool {
		_, waits := waitedAt[task.NodeID]
		return !waits
	}
	listed := p.listedOpen()
	for _, task := range listed {
		if nobodyWaits(task) {
			p.warn("“%s” is still open though the instance is not waiting there; it will be withdrawn.", taskName(task))
		}
	}
	if more := countOf(p.open[len(listed):], nobodyWaits); more > 0 {
		p.warn("%d more open %s on steps the instance is not waiting at; %s will be withdrawn.", more, taskOrTasksAre(more), itOrThey(more))
	}
}

// itOrThey is the word for one task or for several.
func itOrThey(count int) string {
	if count == 1 {
		return "it"
	}
	return "they"
}

// warnOfTheCaller says, of a called instance that is cancelled alone, that the
// process that started it is not resumed. Said only of a caller that has not
// ended: one that has is waiting for nothing, and cannot be cancelled or held
// next.
func (s *instanceDeviationService) warnOfTheCaller(ctx context.Context, p *planning) error {
	caller := p.instance.ParentInstance
	if caller == nil {
		return nil
	}
	parent, err := s.engine.GetInstance(ctx, caller.ID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the instance that started instance %s: %w", p.instance.ID, err)
	}
	if !hasEnded(parent.Status) {
		p.warn("This instance was started by another process (instance %s), which is still waiting for it and is not resumed by this; "+
			"cancel or hold that one next.", caller.ID)
	}
	return nil
}

// calledAndNotEnded is the processes an instance started that have not ended
// — those one step started, when a step is named — by id, in order. One that
// is suspended has not ended: it can be resumed, and would then be running
// under an instance that is gone.
func (s *instanceDeviationService) calledAndNotEnded(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]uuid.UUID, error) {
	called, err := s.engine.ListSubProcesses(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the processes instance %s started: %w", instanceID, err)
	}
	var notEnded []uuid.UUID
	for _, child := range called {
		if hasEnded(child.Status) {
			continue
		}
		if nodeID != "" && (child.ParentNode == nil || child.ParentNode.ID != nodeID) {
			continue
		}
		notEnded = append(notEnded, child.ID)
	}
	slices.SortFunc(notEnded, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return notEnded, nil
}
