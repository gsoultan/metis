package impl

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// maxNamesListed is how many names a sentence of a plan spells out before it
// says how many more there are. The names come from a process somebody wrote
// — its fields, the instances it started — and a sentence is for reading.
const maxNamesListed = 10

// planning is one plan being made: what was read of the instance, and the
// plan so far.
type planning struct {
	instance entities.ProcessInstance
	command  entities.DeviationCommand
	// def is the graph the instance runs.
	def *entities.ProcessDefinition
	// node is the step the command names: nil when it names none, and when
	// the process has no such step.
	node *entities.Node
	// open is the open tasks where the command acts — on the step, or
	// anywhere on the instance for a cancel that names no step — in the order
	// of their ids.
	open []models.TaskModel
	plan entities.DeviationPlan
}

// plan says what a command would do to an instance as it stands, and every
// reason it cannot be done.
//
// It reads and decides nothing. Every refusal that is true is collected, not
// the first one, so one preview shows everything there is to fix; each is a
// sentence a process owner can act on, and names a step by its name.
//
// instance is the caller's: the row a preview read, or the row an apply
// locked. Everything else is read here, without a lock.
func (s *instanceDeviationService) plan(ctx context.Context, instance entities.ProcessInstance, command entities.DeviationCommand) (entities.DeviationPlan, error) {
	p, err := s.startPlanning(ctx, instance, command)
	if err != nil {
		return entities.DeviationPlan{}, err
	}
	p.refuseWhereItStands()
	switch command.Kind {
	case entities.DeviationWaive:
		err = s.planWaive(ctx, p)
	case entities.DeviationCancel:
		err = s.planCancel(ctx, p)
	case entities.DeviationHold:
		err = s.planHold(ctx, p)
	default:
		err = fmt.Errorf("planning for instance %s: %q is not something done to an instance in place", instance.ID, command.Kind)
	}
	if err != nil {
		return entities.DeviationPlan{}, err
	}
	return p.plan, nil
}

// startPlanning reads what every kind of plan needs — the graph the instance
// runs and the work open where the command acts — and fills in what a plan
// says whatever it goes on to refuse: the step, the open work, the visit key.
func (s *instanceDeviationService) startPlanning(ctx context.Context, instance entities.ProcessInstance, command entities.DeviationCommand) (*planning, error) {
	if instance.Definition == nil {
		return nil, fmt.Errorf("planning for instance %s: it names no version of a process", instance.ID)
	}
	def, err := s.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return nil, fmt.Errorf("reading the process instance %s runs: %w", instance.ID, err)
	}
	if def == nil {
		return nil, fmt.Errorf("reading the process instance %s runs: there is no version %s", instance.ID, instance.Definition.ID)
	}
	open, err := s.openWhereItActs(ctx, instance.ID, command.NodeID)
	if err != nil {
		return nil, err
	}
	p := &planning{instance: instance, command: command, def: def, open: open}
	p.plan = entities.DeviationPlan{
		InstanceID: instance.ID, Kind: command.Kind, Scope: deviationScopeOf(command.Kind),
		NodeID: command.NodeID, Outputs: command.Outputs,
	}
	if command.NodeID != "" {
		if p.node = def.FindNode(command.NodeID); p.node != nil {
			p.plan.NodeName = cmp.Or(p.node.Name, p.node.ID)
		}
	}
	// The key is made from exactly the tasks the plan lists, so what an
	// administrator was shown is what the key stands for.
	ids := make([]uuid.UUID, 0, len(open))
	for _, task := range open {
		ids = append(ids, uuid.UUID(task.ID))
		p.plan.OpenWork = append(p.plan.OpenWork, entities.DeviationOpenWork{
			TaskID: uuid.UUID(task.ID), Name: taskName(task), Status: entities.TaskStatus(task.Status),
			Assignee: task.Assignee, IterationID: task.IterationID,
		})
	}
	p.plan.VisitKey = deviationVisitKey(instance, command.Kind, command.NodeID, ids)
	return p, nil
}

// openWhereItActs is the tasks still somebody's to do where a command acts:
// on the step it names, or anywhere on the instance when it names none. They
// are sorted by id, so the same work reads the same way twice.
//
// The tasks are read as they stand, with no row held: a plan is a preview,
// and a preview makes nobody wait. So what it lists can be claimed, handed
// over or finished a moment later. It is what an administrator is shown and
// what the visit key is made from — not what was withdrawn. The record of an
// act is written from the rows the act itself held (nodeActions.waive and
// cancel return them), never from a plan's open work.
func (s *instanceDeviationService) openWhereItActs(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]models.TaskModel, error) {
	tasks, err := s.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the tasks of instance %s: %w", instanceID, err)
	}
	var open []models.TaskModel
	for _, task := range tasks {
		if openTask(task.Status) && (nodeID == "" || task.NodeID == nodeID) {
			open = append(open, task)
		}
	}
	slices.SortFunc(open, func(a, b models.TaskModel) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	return open, nil
}

// refuse adds a reason the command cannot be applied.
func (p *planning) refuse(format string, args ...any) {
	p.plan.Refusals = append(p.plan.Refusals, fmt.Sprintf(format, args...))
}

// warn adds something whoever applies the command should know first.
func (p *planning) warn(format string, args ...any) {
	p.plan.Warnings = append(p.plan.Warnings, fmt.Sprintf(format, args...))
}

// refuseWhereItStands is what every kind asks: that the instance is running,
// that it waits at the step the command names, and that somebody said why.
func (p *planning) refuseWhereItStands() {
	if p.instance.Status != entities.ProcessActive {
		p.refuse("This instance is %s; only a running instance can be %s.", p.instance.Status, pastTense(p.command.Kind))
	}
	switch {
	case p.command.NodeID == "":
		// A cancel that names no step: planCancel says where the instance
		// waits, if it waits anywhere.
	case p.node == nil:
		p.refuse("This process has no step %q.", p.command.NodeID)
	case len(p.instance.GetTokensByNode(p.node)) == 0:
		p.refuse("This instance is not waiting at “%s”.", p.plan.NodeName)
	}
	// Counted as the ledger counts it: characters, after the spaces around it
	// are dropped.
	switch reason := strings.TrimSpace(p.command.Reason); {
	case reason == "":
		p.refuse("Say why: a reason is required, and it is kept with the record.")
	case utf8.RuneCountInString(reason) > entities.MaxDeviationReasonLength:
		p.refuse("The reason is longer than %d characters; say it more briefly.", entities.MaxDeviationReasonLength)
	}
}

// warnWhoLosesWork says whose work a waive or a cancel would take. Each is
// told when it happens; the administrator is told before.
func (p *planning) warnWhoLosesWork() {
	for _, task := range p.open {
		if task.Assignee != "" && (task.Status == models.TaskClaimed || task.Status == models.TaskDelegated) {
			p.warn("“%s” is with %s, who will be told it was withdrawn.", taskName(task), task.Assignee)
		}
	}
}

// planHold asks nothing further of a hold, and warns when the step is held
// already: nodeActions.hold raises no second incident for a step that has one
// open, and this is the same question it asks.
func (s *instanceDeviationService) planHold(ctx context.Context, p *planning) error {
	if p.node == nil {
		return nil
	}
	incidents, err := s.repo.Incident().ListByInstance(ctx, p.instance.ID)
	if err != nil {
		return fmt.Errorf("reading the incidents on instance %s: %w", p.instance.ID, err)
	}
	for _, incident := range incidents {
		if incident.NodeID == p.node.ID && incident.Status == models.IncidentOpen {
			p.warn("“%s” already has an open incident; the hold will use it.", p.plan.NodeName)
			return nil
		}
	}
	return nil
}

// deviationScopeOf is how much of an instance a kind of in-place act reaches:
// a waive the step's work, a cancel and a hold the instance.
func deviationScopeOf(kind entities.DeviationKind) entities.DeviationScope {
	if kind == entities.DeviationWaive {
		return entities.DeviationScopeTask
	}
	return entities.DeviationScopeInstance
}

// pastTense is what an instance or a step is once an act of this kind is
// made: waived, cancelled, held.
func pastTense(kind entities.DeviationKind) string {
	switch kind {
	case entities.DeviationWaive:
		return "waived"
	case entities.DeviationCancel:
		return "cancelled"
	case entities.DeviationHold:
		return "held"
	}
	return string(kind)
}

// taskName is what a task is called: its own name, or its step's id when it
// has none.
func taskName(task models.TaskModel) string {
	return cmp.Or(task.Name, task.NodeID)
}

// idsAsText is ids as they are written.
func idsAsText(ids []uuid.UUID) []string {
	text := make([]string, len(ids))
	for i, id := range ids {
		text[i] = id.String()
	}
	return text
}

// listed sets names out for a sentence: "a", "a and b", "a, b and c", and
// past maxNamesListed the first of them and how many more there are.
func listed(names []string) string {
	switch {
	case len(names) == 0:
		return ""
	case len(names) == 1:
		return names[0]
	case len(names) > maxNamesListed:
		return fmt.Sprintf("%s and %d more", strings.Join(names[:maxNamesListed], ", "), len(names)-maxNamesListed)
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " and " + names[last]
}

// itOrThem is the word for one thing or for several.
func itOrThem(count int) string {
	if count == 1 {
		return "it"
	}
	return "them"
}
