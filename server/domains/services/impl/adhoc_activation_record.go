package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// auditSubProcess is the key under which an activation's audit entry names the
// ad-hoc sub-process the step was started in; its ledger row keeps the same
// under the same name, in its details.
const auditSubProcess = "sub_process"

// recordActivation writes who started a step inside an ad-hoc sub-process: a
// row in the instance's deviation ledger and an entry on its trail, each naming
// the other. The entry is given its id here, so the row can point at it before
// it exists.
//
// It runs in the activation's unit of work, after the step has executed, under
// the instance lock the activation took first. It reads nothing but what the
// ledger's own insert checks and takes no other row's lock, so the order stays
// instance first.
//
// Its error is the activation's: a step whose start cannot be recorded is not
// started, and the token and whatever the step opened roll back with it.
func (a *adHocActivator) recordActivation(
	ctx context.Context,
	instance entities.ProcessInstance,
	def *entities.ProcessDefinition,
	subProcess, target *entities.Node,
	reason string,
) error {
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("activate %q: %w", target.ID, err)
	}
	row := activationDeviation(instance, def, subProcess, target, activationActor(ctx), reason)
	row.AuditEntryID = auditID
	recorded, err := a.ledger.Record(ctx, row)
	if err != nil {
		return fmt.Errorf("activate %q: recording who started it: %w", target.ID, err)
	}
	entry := activationEntry(recorded, subProcess, target)
	entry.ID = auditID
	if err := a.audit.RecordEvent(ctx, entry); err != nil {
		return fmt.Errorf("activate %q: recording who started it: %w", target.ID, err)
	}
	return nil
}

// activationActor is who started the step: the signed-in account, or "System"
// when nobody is signed in — the server acting for itself, named as a
// migration with no actor is (migrationActor).
func activationActor(ctx context.Context) string {
	if account := signedIn(ctx); account != nil && strings.TrimSpace(account.Username) != "" {
		return account.Username
	}
	return "System"
}

// activationDeviation is the ledger row of one activation. Starting a step
// inside an ad-hoc sub-process is the process working as designed, so the row
// changes nothing it could show a before and after of: it says who chose the
// step, in which sub-process, and why when they said.
func activationDeviation(
	instance entities.ProcessInstance,
	def *entities.ProcessDefinition,
	subProcess, target *entities.Node,
	actor, reason string,
) entities.Deviation {
	row := entities.Deviation{
		Instance: &entities.ProcessInstance{ID: instance.ID},
		Kind:     entities.DeviationAdHocActivation,
		Scope:    entities.DeviationScopeTask,
		Origin:   entities.DeviationOriginAdHoc,
		Status:   entities.DeviationApplied,
		Node:     &entities.Node{ID: target.ID, Name: target.Name},
		Actor:    actor,
		Reason:   reason,
		Details:  map[string]any{auditSubProcess: subProcess.ID},
	}
	if instance.Project != nil {
		row.Project = &entities.Project{ID: instance.Project.ID}
	}
	if def != nil && def.ID != uuid.Nil {
		row.Definition = &entities.ProcessDefinition{ID: def.ID}
	}
	return row
}

// activationEntry is the trail entry of the activation recorded as row. It
// tells the reason as the ledger kept it, so the two never differ by a space.
func activationEntry(row entities.Deviation, subProcess, target *entities.Node) entities.AuditEntry {
	narrative := fmt.Sprintf("%s started “%s” inside “%s”", row.Actor, nameOrID(target), nameOrID(subProcess))
	data := map[string]any{
		auditActor:       row.Actor,
		auditSubProcess:  subProcess.ID,
		"run_id":         row.RunID.String(),
		auditDeviationID: row.ID.String(),
	}
	if row.Reason != "" {
		data[auditReason] = row.Reason
		narrative += ": " + row.Reason
	}
	return entities.AuditEntry{
		Type:      EventStepActivated,
		Message:   fmt.Sprintf("activated %s inside %s", target.ID, subProcess.ID),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data:      data,
		Project:   row.Project,
		Instance:  row.Instance,
		Node:      target,
	}
}

// nameOrID is a step's name, or its id for a step nobody named.
func nameOrID(node *entities.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return node.ID
}
