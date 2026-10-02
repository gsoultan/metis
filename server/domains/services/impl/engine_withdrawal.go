package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// endActivity ends every running instance of an activity on this instance.
//
// Four things make an activity running, and all four go: its tokens — one per
// iteration, on a step that repeats — the count of its iterations, the tasks
// it has open in somebody's inbox, each withdrawn and announced, and the work
// it has parked for an outside worker.
//
// The ways an activity ends without finishing come here, so they cannot
// disagree about what ending means: an interrupting boundary event, and a
// completion condition that is met while iterations are still open. A deadline
// used to take the tokens and the tasks and leave the count, a completion
// condition took the count and left the rest, and neither took the work parked
// for a worker — which the worker then finished, for a step that refused it.
//
// A process the activity called is not ended here: nothing in the engine ends
// an instance from outside it. It runs on, and its return finds no token
// waiting for it — see EndEventHandler.resumeParent.
//
// The instance is not saved here: the caller is part-way through an advance
// and saves it as that advance does.
func (e *Engine) endActivity(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if node == nil {
		return nil
	}
	instance.RemoveTokenByNode(node)
	instance.FinishMultiInstance(node.ID)
	if err := e.cancelOpenTasksForNode(ctx, instance, node); err != nil {
		return err
	}
	return e.withdrawExternalTasksFor(ctx, instance, node)
}

// cancelOpenTasksForNode withdraws any task still open for node.
//
// Removing an activity's token cancels it as far as the engine is concerned,
// but the user task it created is a separate row and nothing was closing it.
// The work stayed in whoever's inbox it was assigned to, and completing it acted
// on an activity the process had already abandoned.
//
// A failure here is returned: an interrupt that half-happened — token gone, task
// still offered — is worse than one that reports itself.
func (e *Engine) cancelOpenTasksForNode(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if node == nil {
		return nil
	}
	return e.cancelOpenTasksOn(ctx, instance, []*entities.Node{node})
}

// cancelOpenTasksOn withdraws the tasks still open on any of nodes, reading
// the instance's tasks once however many nodes there are.
func (e *Engine) cancelOpenTasksOn(ctx context.Context, instance *entities.ProcessInstance, nodes []*entities.Node) error {
	byID := make(map[string]*entities.Node, len(nodes))
	for _, node := range nodes {
		if node != nil {
			byID[node.ID] = node
		}
	}
	if len(byID) == 0 {
		return nil
	}

	ms, err := e.repo.Task().ListByInstance(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("list tasks for instance %s: %w", instance.ID, err)
	}
	for i := range ms {
		m := &ms[i]
		node, ours := byID[m.NodeID]
		if !ours || !openTask(m.Status) {
			continue
		}
		if err := e.repo.Task().UpdateStatus(ctx, uuid.UUID(m.ID), models.TaskCanceled); err != nil {
			return fmt.Errorf("cancel task %s on node %s: %w", uuid.UUID(m.ID), node.ID, err)
		}
		e.dispatcher.Dispatch(ctx, entities.ProcessEvent{
			Type:      entities.EventTaskCanceled,
			Instance:  instance,
			Project:   instance.Project,
			Node:      node,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
			// Who it was taken from. The task row is in hand here and the
			// person holding it is the only one who needs telling — the node's
			// assignee is who the diagram nominated, which is not the same
			// thing once somebody has claimed it.
			Assignee: m.Assignee,
		})
	}
	return nil
}

// withdrawExternalTasksFor takes the work node has parked for an outside
// worker off the list workers fetch from.
//
// An external task is a row of its own, like a user task, and it outlived the
// activity it was created for. A worker went on being offered it; completing it
// was refused, because the step had finished, and rolled back — so its lock ran
// out and it was offered again, and refused again, for as long as the instance
// existed.
//
// The row is deleted, which is what completing one does: the list holds only
// work that is still wanted. A worker that had already fetched one finds it
// gone when it reports back, and is told there is no such task. What tells the
// two apart afterwards is the instance's trail: see recordParkedWorkWithdrawn.
//
// Called with the instance held, so everything else that takes a task's row
// and the instance has to take the instance first — see
// externalTaskService.Complete.
//
// A failure is returned, for the reason cancelOpenTasksForNode returns one.
func (e *Engine) withdrawExternalTasksFor(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	return e.withdrawExternalTasksOn(ctx, instance, []*entities.Node{node})
}

// WithdrawParkedWork is withdrawExternalTasksFor for a caller outside the
// engine: a worker's report that finds its step is no longer waiting for it
// (externalTaskService). The step ended without withdrawing the work — a
// release before migration 31 did not — so it is withdrawn when it is found.
func (e *Engine) WithdrawParkedWork(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if instance == nil || node == nil {
		return nil
	}
	return e.withdrawExternalTasksFor(ctx, instance, node)
}

// withdrawExternalTasksOn withdraws the work parked for workers on any of
// nodes, reading the instance's parked work once however many nodes there are.
//
// Each node that had work parked gets its own line on the trail, in the order
// the nodes were given: the line is about a step, and several can end at once
// when what ends is the sub-process they are inside.
func (e *Engine) withdrawExternalTasksOn(ctx context.Context, instance *entities.ProcessInstance, nodes []*entities.Node) error {
	ours := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if node != nil {
			ours[node.ID] = true
		}
	}
	if len(ours) == 0 {
		return nil
	}

	parked, err := e.repo.ExternalTask().ListByProcessInstance(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("list external tasks for instance %s: %w", instance.ID, err)
	}
	withdrawn := make(map[string][]string)
	for _, task := range parked {
		if !ours[task.NodeID] {
			continue
		}
		if err := e.repo.ExternalTask().Delete(ctx, uuid.UUID(task.ID)); err != nil {
			return fmt.Errorf("withdraw external task %s on node %s: %w", uuid.UUID(task.ID), task.NodeID, err)
		}
		withdrawn[task.NodeID] = append(withdrawn[task.NodeID], uuid.UUID(task.ID).String())
	}
	for _, node := range nodes {
		if node == nil || len(withdrawn[node.ID]) == 0 {
			continue
		}
		if err := e.recordParkedWorkWithdrawn(ctx, instance, node, withdrawn[node.ID]); err != nil {
			return err
		}
		// A node given twice is recorded once.
		delete(withdrawn, node.ID)
	}
	return nil
}

// recordParkedWorkWithdrawn writes, on the instance's trail, that a step ended
// with work still parked for workers and that the work was taken back.
//
// One line for the step, however much work it had parked: the question asked
// later is why the step's work disappeared, not what became of each piece. The
// line names the step as the diagram does; which tasks they were is in the
// entry's data, for whoever is asked by a worker why its task is gone.
//
// It is written by the engine rather than raised as an event, because a
// task-withdrawn event is addressed to the person who held the task and
// nobody holds this one.
//
// A line that cannot be written is returned, and the step does not end. The
// line is written in the transaction that withdraws the work, so the two are
// kept or lost together: the row goes either way a task ends, and work
// withdrawn with no line saying so reads, for good, as work somebody did.
func (e *Engine) recordParkedWorkWithdrawn(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node, taskIDs []string) error {
	stepName := "A step"
	if node.Name != "" {
		stepName = fmt.Sprintf("'%s'", node.Name)
	}
	work := "piece of work it had waiting for a worker was"
	if len(taskIDs) != 1 {
		work = "pieces of work it had waiting for a worker were"
	}
	entry := entities.AuditEntry{
		Project:  instance.Project,
		Instance: &entities.ProcessInstance{ID: instance.ID},
		Node:     node,
		Type:     EventParkedWorkWithdrawn,
		Message:  "Work parked for workers was withdrawn when its step ended",
		Narrative: fmt.Sprintf("%s ended before all of its work was done, so the %d %s withdrawn.",
			stepName, len(taskIDs), work),
		Data: map[string]any{
			"node_id":           node.ID,
			"external_task_ids": taskIDs,
		},
		Timestamp: time.Now(),
	}
	if err := NewAuditWriter(e.repo.Audit()).RecordEvent(ctx, entry); err != nil {
		return fmt.Errorf("record the work withdrawn from node %s of instance %s: %w", node.ID, instance.ID, err)
	}
	return nil
}
