package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// waive withdraws the work parked on one step and advances the instance past
// it as though the step had been performed, and returns the tasks it withdrew
// as they were before.
//
// live is the row its caller locked, and def the graph that instance runs:
// only that graph knows what follows the step. Both writes join the caller's
// unit of work, so an advance that fails — a gateway after the step with no
// branch to take — puts the work back as it was once the caller rolls back.
//
// Work somebody does — a user or a manual task — is ended whole (finishWhole):
// such a step may repeat, and an advance counts one run of it and leaves the
// rest. Every other step is withdrawn from and advanced past as it always
// was, whatever its loop. Which of the two, and the one case that is neither,
// is endsWhole's to say.
func (a nodeActions) waive(
	ctx context.Context,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	nodeID string,
) ([]models.TaskModel, error) {
	whole, err := a.endsWhole(def.FindNode(nodeID), nodeID)
	if err != nil {
		return nil, err
	}
	if whole {
		return a.finishWhole(ctx, live, def, nodeID)
	}
	withdrawn, err := a.withdrawOn(ctx, live.ID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := a.engine.Proceed(ctx, live, def, nodeID); err != nil {
		return nil, fmt.Errorf("advancing instance %s past %q: %w", live.ID, nodeID, err)
	}
	return withdrawn, nil
}

// finishWhole ends every run of a step and advances past it once, and returns
// the tasks that were open on it as they were before.
//
// The tasks are read here and withdrawn by the engine: ending an activity is
// the engine's to do, and it tells whoever held each task as it withdraws it.
// Reading first is what lets the record say what was withdrawn — afterwards
// they are cancelled rows among the step's other cancelled rows.
//
// They are read with their rows held (heldOpenOn), so the engine's own read
// finds them as they are recorded here: the record and the announcement name
// the same holder, and it is whoever held the task when it was withdrawn.
func (a nodeActions) finishWhole(
	ctx context.Context,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	nodeID string,
) ([]models.TaskModel, error) {
	withdrawn, err := a.heldOpenOn(ctx, live.ID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := a.finisher.FinishActivity(ctx, live, def, nodeID); err != nil {
		return nil, fmt.Errorf("advancing instance %s past %q: %w", live.ID, nodeID, err)
	}
	return withdrawn, nil
}

// isWorkSomebodyDoes reports whether node is a step a person performs: the
// only kind of step whose runs are counted strictly (Node.IsRepeatingApproval),
// and so the only kind ended whole.
func isWorkSomebodyDoes(node *entities.Node) bool {
	return node != nil && (node.Type == entities.UserTask || node.Type == entities.ManualTask)
}

// endsWhole says how a skipped step is ended: whole, by an engine that can
// (true), or withdrawn from and advanced past (false).
//
// With no such engine, work somebody does that does not repeat is advanced
// past: for one run the two come to the same thing. A repeating approval is
// refused. Advancing past it counts one run and leaves the others — their
// tokens on the step, their tasks withdrawn, a sequential one's next run
// started — and says nothing; that is the defect ending a step whole exists
// to remove, and a server wired so that it comes back has to hear about it.
//
// The refusal is a plain error: it is how the server was put together, and
// nothing the person who asked for the skip can change by asking differently.
func (a nodeActions) endsWhole(node *entities.Node, nodeID string) (bool, error) {
	if !isWorkSomebodyDoes(node) {
		return false, nil
	}
	if a.finisher != nil {
		return true, nil
	}
	if node.IsRepeatingApproval() {
		return false, fmt.Errorf("skipping %q: the step is done once for each of several people, "+
			"and this server's engine cannot end all of those runs together, so skipping it would end one and leave the rest", nodeID)
	}
	return false, nil
}
