package impl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A process is somebody's input and a plan goes back over the wire, so a plan
// has a size whatever the process: the decision points, the open tasks and
// the called instances it lists, and the steps its sentences name one by one
// before they count the rest. The names
// inside a sentence are cut the way a refused completion cuts them
// (shownNames).
const (
	maxDecisionPointsListed  = 100
	maxPointsNamed           = 10
	maxOpenWorkListed        = 200
	maxCalledInstancesListed = 200
)

// planning is one plan being made: what was read of the instance, and the
// plan so far.
type planning struct {
	instance entities.ProcessInstance
	command  entities.DeviationCommand
	// def is the graph the instance runs.
	def *entities.ProcessDefinition
	// node is the step the command names: nil when it names none, and when
	// the version the instance runs has no such step.
	node *entities.Node
	// open is the open tasks where the command acts — on the step for a waive
	// and a hold, anywhere on the instance for a cancel — in the order of
	// their ids.
	open []models.TaskModel
	// stepIncidents is the incidents on the step a hold names, open and
	// resolved. Read once, for a hold only: its key covers them, and its plan
	// warns of one that is open.
	stepIncidents []models.IncidentModel
	plan          entities.DeviationPlan
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
		p.planHold()
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
	def, err := s.graphRunBy(ctx, instance.ID, instance.Definition.ID)
	if err != nil {
		return nil, err
	}
	open, err := s.openWhereItActs(ctx, instance.ID, command)
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
			// As the ledger keeps a step's name, so that the plan, the row
			// and the trail entry made from the plan name it alike.
			p.plan.NodeName = shownStepName(cmp.Or(p.node.Name, p.node.ID))
		} else if p.cancelsWhereTheVersionHasNoStep() {
			// The version has no name for it: it is shown by its id.
			p.plan.NodeName = shownStepName(command.NodeID)
		}
	}
	if command.Kind == entities.DeviationHold {
		if p.stepIncidents, err = s.incidentsOn(ctx, instance.ID, command.NodeID); err != nil {
			return nil, err
		}
	}
	p.plan.VisitKey = p.visitKey()
	p.listOpenWork()
	return p, nil
}

// incidentsOn is the incidents an instance has on one step, open and resolved.
func (s *instanceDeviationService) incidentsOn(ctx context.Context, instanceID uuid.UUID, nodeID string) ([]models.IncidentModel, error) {
	incidents, err := s.repo.Incident().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the incidents on instance %s: %w", instanceID, err)
	}
	var on []models.IncidentModel
	for _, incident := range incidents {
		if incident.NodeID == nodeID {
			on = append(on, incident)
		}
	}
	return on, nil
}

// visitKey is the key of the work the plan is made for: every open task where
// the command acts, whether or not the plan lists it — and, for a hold, the
// incidents on the step. What is listed is for reading; what is keyed is what
// the act would take.
func (p *planning) visitKey() string {
	ids := make([]uuid.UUID, 0, len(p.open))
	for _, task := range p.open {
		ids = append(ids, uuid.UUID(task.ID))
	}
	return deviationVisitKey(p.instance, p.command.Kind, p.command.NodeID, ids, p.stepIncidents)
}

// listOpenWork puts the open tasks where the command acts into the plan: the
// first maxOpenWorkListed of them, and how many there are. How much work an
// instance has open is the instance's to say — a step done once for each of a
// thousand people has a thousand tasks — and a plan is for reading. The visit
// key is made from every one of them, and the act withdraws every one.
func (p *planning) listOpenWork() {
	p.plan.OpenWorkInAll = len(p.open)
	for _, task := range p.listedOpen() {
		p.plan.OpenWork = append(p.plan.OpenWork, entities.DeviationOpenWork{
			TaskID: uuid.UUID(task.ID), Name: taskName(task), NodeID: task.NodeID, NodeName: p.stepName(task.NodeID),
			Status: entities.TaskStatus(task.Status), Assignee: task.Assignee, IterationID: task.IterationID,
		})
	}
	if more := len(p.open) - len(p.plan.OpenWork); more > 0 {
		p.warn("%d more %s open and %s not listed here.", more, taskOrTasksAre(more), isOrAre(more))
	}
}

// listedOpen is the open tasks a plan lists and speaks of one by one; the
// rest are counted.
func (p *planning) listedOpen() []models.TaskModel {
	return p.open[:min(len(p.open), maxOpenWorkListed)]
}

// stepShown is the step the command names, as a sentence names it.
func (p *planning) stepShown() string {
	return shownStepName(p.plan.NodeName)
}

// shownStepName is what a step or a task is called, as a plan shows it: the
// first deviationNodeNameLength characters, cut between characters, which is
// how the ledger keeps a step's name (deviationNode).
//
// One rule, wherever a plan names a step — its own step, a task's, a decision
// point's (decisionScan.pointsAt): the step's name, or its id where it has
// none, and either is cut alike. A definition's author chooses both, and
// neither has a length. The id standing in for a name is a name there; the
// id itself is always beside it, whole, in the field that carries the id
// (NodeID), and that is the one to act on.
func shownStepName(name string) string {
	if utf8.RuneCountInString(name) <= deviationNodeNameLength {
		return name
	}
	return string([]rune(name)[:deviationNodeNameLength])
}

// graphRunBy reads the version of a process an instance runs: the graph a
// plan is made over, and the one an act advances the instance along.
func (s *instanceDeviationService) graphRunBy(ctx context.Context, instanceID, versionID uuid.UUID) (*entities.ProcessDefinition, error) {
	def, err := s.engine.GetProcessDefinition(ctx, versionID)
	if errors.Is(err, apierr.ErrNotFound) {
		// The caller may read the instance, so this is not "no such instance":
		// an instance whose version is gone is the server's trouble, and is
		// answered as that — not with the not-found the read of the version
		// gave, which would say the instance is not there. The cause is kept
		// as words and deliberately not wrapped.
		return nil, fmt.Errorf("reading the process instance %s runs: version %s is not there (%s)", instanceID, versionID, err.Error())
	}
	if err != nil {
		return nil, fmt.Errorf("reading the process instance %s runs: %w", instanceID, err)
	}
	if def == nil {
		return nil, fmt.Errorf("reading the process instance %s runs: version %s is not there", instanceID, versionID)
	}
	return def, nil
}

// openWhereItActs is the tasks still somebody's to do where a command acts,
// sorted by id so the same work reads the same way twice: those on the step
// for a waive and a hold, and every one the instance has for a cancel. A
// cancel withdraws them all whichever step it names, and an administrator is
// shown everything the act would take.
//
// The tasks are read as they stand, with no row held: a plan is a preview,
// and a preview makes nobody wait. So what it lists can be claimed, handed
// over or finished a moment later. It is what an administrator is shown and
// what the visit key is made from — not what was withdrawn. The record of an
// act is written from the rows the act itself held (nodeActions.waive and
// cancel return them), never from a plan's open work.
func (s *instanceDeviationService) openWhereItActs(ctx context.Context, instanceID uuid.UUID, command entities.DeviationCommand) ([]models.TaskModel, error) {
	tasks, err := s.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the tasks of instance %s: %w", instanceID, err)
	}
	everywhere := command.Kind == entities.DeviationCancel || command.NodeID == ""
	var open []models.TaskModel
	for _, task := range tasks {
		if openTask(task.Status) && (everywhere || task.NodeID == command.NodeID) {
			open = append(open, task)
		}
	}
	slices.SortFunc(open, func(a, b models.TaskModel) int { return byID(uuid.UUID(a.ID), uuid.UUID(b.ID)) })
	return open, nil
}

// stepName is what the step with an id is called, as a plan shows it: its
// name, or the id for a step with no name and for one the process no longer
// has (shownStepName).
func (p *planning) stepName(nodeID string) string {
	if node := p.def.FindNode(nodeID); node != nil && node.Name != "" {
		return shownStepName(node.Name)
	}
	return shownStepName(nodeID)
}

// refuse adds a reason the command cannot be applied.
func (p *planning) refuse(format string, args ...any) {
	p.plan.Refusals = append(p.plan.Refusals, fmt.Sprintf(format, args...))
}

// warn adds something whoever applies the command should know first.
func (p *planning) warn(format string, args ...any) {
	p.plan.Warnings = append(p.plan.Warnings, fmt.Sprintf(format, args...))
}

// cancelsWhereTheVersionHasNoStep reports whether the command is a cancel
// naming a step the instance holds a token on and its version does not have.
//
// A migration of an earlier release could move an instance onto a version
// without the step it was on, token and all (docs/upgrading.md). Nothing else
// reaches such an instance: its step cannot be completed into anything, and a
// cancel that names no step is refused because it does wait somewhere. A
// cancel ends the whole instance whichever step it names, and asks nothing of
// the step but where the instance stood, so it may name this one. A waive and
// a hold act on the step itself, and need the version to have it.
func (p *planning) cancelsWhereTheVersionHasNoStep() bool {
	return p.command.Kind == entities.DeviationCancel && p.node == nil && p.command.NodeID != "" &&
		len(p.instance.GetTokensByNode(&entities.Node{ID: p.command.NodeID})) > 0
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
	case p.cancelsWhereTheVersionHasNoStep():
		// The instance waits there, and that is all a cancel asks of a step.
	case p.node == nil:
		p.refuse("This process has no step %q.", p.command.NodeID)
	case len(p.instance.GetTokensByNode(p.node)) == 0:
		p.refuse("This instance is not waiting at “%s”.", p.stepShown())
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
//
// The tasks the plan lists are spoken of one by one, and the rest counted.
func (p *planning) warnWhoLosesWork() {
	held := func(task models.TaskModel) bool {
		return task.Assignee != "" && (task.Status == models.TaskClaimed || task.Status == models.TaskDelegated)
	}
	listed := p.listedOpen()
	for _, task := range listed {
		if held(task) {
			p.warn("“%s” is with %s, who will be told it was withdrawn.", taskName(task), task.Assignee)
		}
	}
	if more := countOf(p.open[len(listed):], held); more > 0 {
		p.warn("%d more open %s with somebody, who will be told %s withdrawn.", more, taskOrTasksAre(more), itWasOrTheyWere(more))
	}
}

// countOf is how many tasks are so.
func countOf(tasks []models.TaskModel, so func(models.TaskModel) bool) int {
	count := 0
	for _, task := range tasks {
		if so(task) {
			count++
		}
	}
	return count
}

// planHold asks nothing further of a hold, and warns when the step is held
// already: nodeActions.hold raises no second incident for a step that has one
// open, and this is the same question it asks, of the incidents the key was
// made from.
func (p *planning) planHold() {
	if p.node == nil {
		return
	}
	for _, incident := range p.stepIncidents {
		if incident.Status == models.IncidentOpen {
			p.warn("“%s” already has an open incident; the hold will use it.", p.stepShown())
			return
		}
	}
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

// taskName is what a task is called, as a plan shows it: its own name, or its
// step's id when it has none.
func taskName(task models.TaskModel) string {
	return shownStepName(cmp.Or(task.Name, task.NodeID))
}

// idsAsText is ids as they are written.
func idsAsText(ids []uuid.UUID) []string {
	text := make([]string, len(ids))
	for i, id := range ids {
		text[i] = id.String()
	}
	return text
}

// namesShownOf is namesShown for a list that holds only the first of inAll
// names: what it holds, and how many more there are.
func namesShownOf(names []string, inAll int) string {
	shown, _ := shownNames(names)
	for i, name := range shown {
		if name == "" {
			shown[i] = `""`
		}
	}
	return joinShown(shown, max(inAll, len(names))-len(shown))
}

// namesShown words names for a sentence of a plan, as a refused completion
// words them (shownNames): the first few, each cut to a length somebody would
// read, and how many more. A name that is empty is shown as one — "" — and
// never as a gap.
func namesShown(names []string) string {
	shown, more := shownNames(names)
	for i, name := range shown {
		if name == "" {
			shown[i] = `""`
		}
	}
	return joinShown(shown, more)
}

// quotedNamesShown is namesShown for the names of steps, each in the quotes a
// step's name is shown in.
func quotedNamesShown(names []string) string {
	shown, more := shownNames(names)
	for i, name := range shown {
		shown[i] = "“" + name + "”"
	}
	return joinShown(shown, more)
}

// joinShown sets out names already cut for showing, and how many were left
// out.
func joinShown(shown []string, more int) string {
	text := strings.Join(shown, ", ")
	if more > 0 {
		text = fmt.Sprintf("%s and %d more", text, more)
	}
	return text
}

// taskOrTasksAre and itWasOrTheyWere word a count of tasks.
func taskOrTasksAre(count int) string {
	if count == 1 {
		return "task is"
	}
	return "tasks are"
}

func itWasOrTheyWere(count int) string {
	if count == 1 {
		return "it was"
	}
	return "they were"
}

// itOrThem is the word for one thing or for several.
func itOrThem(count int) string {
	if count == 1 {
		return "it"
	}
	return "them"
}
