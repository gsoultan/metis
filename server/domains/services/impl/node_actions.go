package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// nodeActions is what skipping a step, cancelling an instance and holding one
// do, written once so that everything that decides to do one of them does the
// same thing.
//
// It holds effects only. Whether to act is its caller's question, asked of the
// row its caller locked: nothing here takes a lock, opens the caller's unit of
// work, or asks whether an instance is running, which version it runs or where
// it waits. Handing an effect a row that was read before the lock is the
// caller's mistake, and nothing here can catch it.
type nodeActions struct {
	repo repositories.Repository
	// engine advances an instance past a step and tells whoever held a task
	// that it is gone. nil in wirings that predate node actions: nothing is
	// announced, and the caller refuses a skip before it gets here.
	engine servicecontracts.ExecutionEngine
	// finisher is the same engine, asked for the one thing an advance cannot
	// do: end a step whole, every run of one that repeats. nil when the engine
	// is not one that can (or there is none), and a step is then advanced past
	// as it was before there was a finisher.
	finisher servicecontracts.ActivityFinisher
	// audit writes the trail entry of an action. nil only in wirings with no
	// audit repository (tests), where the ledger row is the whole record.
	audit servicecontracts.AuditWriter
	// ledger records each action in the transaction that makes it.
	ledger servicecontracts.DeviationRecorder
}

// newNodeActions builds the effects over one repository and one engine, with
// the ledger and the audit writer every caller shares.
func newNodeActions(repo repositories.Repository, engine servicecontracts.ExecutionEngine) nodeActions {
	a := nodeActions{repo: repo, engine: engine, ledger: NewDeviationLedger(repo)}
	if finisher, ok := engine.(servicecontracts.ActivityFinisher); ok {
		a.finisher = finisher
	}
	if repo != nil && repo.Audit() != nil {
		a.audit = NewAuditWriter(repo.Audit())
	}
	return a
}

// withdrawOn cancels the open work parked on one node, and returns those
// tasks as they were before it, for the record of what was withdrawn.
func (a nodeActions) withdrawOn(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]models.TaskModel, error) {
	var withdrawn []models.TaskModel
	err := a.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		tasks, err := a.repo.Task().ListByInstance(txCtx, instanceID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.NodeID != nodeID || !openTask(task.Status) {
				continue
			}
			if err := a.repo.Task().UpdateStatus(txCtx, uuid.UUID(task.ID), models.TaskCanceled); err != nil {
				return err
			}
			a.announceWithdrawal(txCtx, task)
			withdrawn = append(withdrawn, task)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return withdrawn, nil
}

// openOn reads the open work parked on one node and changes none of it: the
// tasks as they stand, for a record written before something else withdraws
// them.
func (a nodeActions) openOn(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]models.TaskModel, error) {
	tasks, err := a.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var open []models.TaskModel
	for _, task := range tasks {
		if task.NodeID == nodeID && openTask(task.Status) {
			open = append(open, task)
		}
	}
	return open, nil
}

// announceWithdrawal says that a task was taken off somebody's list.
//
// Skipping a step and cancelling an instance both cancel work that is in
// somebody's hands. The trail records why, which answers the auditor's question
// but not the assignee's: from their side a task simply disappears, which looks
// the same as somebody else completing it, or as a bug. The engine raises this
// event when a boundary event withdraws a task; a migration withdrawing one is
// the same thing happening for a different reason.
func (a nodeActions) announceWithdrawal(ctx context.Context, task models.TaskModel) {
	if a.engine == nil {
		return
	}
	instance, err := a.engine.GetInstance(ctx, uuid.UUID(task.InstanceID))
	if err != nil {
		log.Warn().Err(err).Str("task", uuid.UUID(task.ID).String()).
			Msg("A withdrawn task could not be announced; whoever held it will not be told")
		return
	}
	// The definition on the instance is a reference rather than the whole
	// graph, so the node has to be read from the definition itself — otherwise
	// the event names the person but not the work, and "a task was withdrawn"
	// with no task in it is not worth sending.
	var node *entities.Node
	if instance.Definition != nil {
		if def, defErr := a.engine.GetProcessDefinition(ctx, instance.Definition.ID); defErr == nil && def != nil {
			node = def.FindNode(task.NodeID)
		}
	}
	a.engine.DispatchEvent(ctx, entities.ProcessEvent{
		Type:      entities.EventTaskCanceled,
		Instance:  &instance,
		Project:   instance.Project,
		Node:      node,
		Timestamp: time.Now().Unix(),
		Variables: instance.Variables,
		Assignee:  task.Assignee,
		Owner:     ownerAwaitingHandBack(task),
	})
}

// record writes the ledger row and the trail entry of one action, in the
// caller's unit of work, and makes them name each other: the entry is given
// its id here, so the row can point at it before it exists.
//
// It answers the row as the ledger wrote it. The row names its entry even in a
// wiring with no audit writer (tests only), where no entry is written. An
// error — from making the entry's id, from the ledger or from the trail —
// comes back as it is; what it means for the action is the caller's to say.
func (a nodeActions) record(ctx context.Context, deviation entities.Deviation, entry entities.AuditEntry) (entities.Deviation, error) {
	auditID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, err
	}
	deviation.AuditEntryID = auditID
	recorded, err := a.ledger.Record(ctx, deviation)
	if err != nil {
		return entities.Deviation{}, err
	}
	if a.audit == nil {
		return recorded, nil
	}
	entry.ID = auditID
	entry.Data[auditDeviationID] = recorded.ID.String()
	if err := a.audit.RecordEvent(ctx, entry); err != nil {
		return entities.Deviation{}, err
	}
	return recorded, nil
}
