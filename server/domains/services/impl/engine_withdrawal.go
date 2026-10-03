package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// endActivity ends an activity that is stopping before it has finished: an
// interrupting boundary event on it has fired, or it is a repeating approval
// whose completion condition was met while runs were still open.
//
// Its tokens go — one per run, on a step that repeats — and the tasks it has
// open in somebody's inbox are withdrawn and announced.
//
// A repeating approval (Node.IsRepeatingApproval) also stops counting its
// runs, so the two ways it can end early agree about what ending means. A
// deadline used to take the tokens and the tasks and leave the count, and a
// completion condition took the count and left the rest; an instance sent
// back to the step found it "already running" and asked nobody.
//
// Every other step is ended as it was before approvals were counted strictly:
// a repeating one keeps its count, and work parked for an outside worker, a
// queued service call and a called process are left as they are. That is
// looser than BPMN asks; it is what those steps did, and it changes when each
// run of a repeating sub-process has tokens of its own and the strict rule can
// be applied to every step.
//
// The instance is not saved here: the caller is part-way through an advance
// and saves it as that advance does.
func (e *Engine) endActivity(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if node == nil {
		return nil
	}
	instance.RemoveTokenByNode(node)
	if node.IsRepeatingApproval() {
		instance.FinishMultiInstance(node.ID)
	}
	return e.cancelOpenTasksForNode(ctx, instance, node)
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

// withdrawExternalTasksOn takes the work any of nodes has parked for an
// outside worker off the list workers fetch from, reading the instance's
// parked work once however many nodes there are.
//
// It is what an ad-hoc sub-process does to the steps still running inside it
// when it finishes (endAdHocSteps), and nothing else withdraws parked work.
//
// An external task is a row of its own, like a user task, and it outlived the
// sub-process it was created in. A worker went on being offered it, and its
// report moved the process on from a step inside a sub-process the instance
// had left.
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
