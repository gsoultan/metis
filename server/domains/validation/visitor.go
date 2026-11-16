package validation

import (
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Visitor implements the entities.DefinitionVisitor interface to validate BPMN definitions.
type Visitor struct {
	errors []string
}

func NewVisitor() *Visitor {
	return &Visitor{
		errors: make([]string, 0),
	}
}

func (v *Visitor) VisitDefinition(pd *entities.ProcessDefinition) {
	// A request whose body omits or misspells "definition" decodes to a nil
	// pointer, so this is reachable from outside rather than only from a
	// programming error. Report it as invalid input; dereferencing it here
	// takes the request down without ever reaching the logging interceptor.
	if pd == nil {
		v.errors = append(v.errors, "No process definition was supplied")
		return
	}
	if pd.Key == "" {
		v.errors = append(v.errors, "Process definition key is missing")
	}
	if len(pd.Nodes) == 0 {
		v.errors = append(v.errors, "Process definition has no nodes")
	}
}

func (v *Visitor) VisitFlowNode(n *entities.Node) {
	if n.ID == "" {
		v.errors = append(v.errors, "Flow node ID is missing")
	}
	if n.Type == "" {
		v.errors = append(v.errors, fmt.Sprintf("Flow node %s has no type", n.ID))
	} else if !n.Type.Known() {
		v.errors = append(v.errors, fmt.Sprintf("Flow node %s has type %q, which the engine cannot run", n.ID, n.Type))
	}
	// The engine runs a script task's script and never a service task's: a
	// service task set to run one was skipped as if it had nothing to call, and
	// the process moved on as though the script had run.
	if n.Type == entities.ServiceTask && n.Implementation() == "script" {
		v.errors = append(v.errors, fmt.Sprintf(
			"Flow node %s is a service task set to run a script, which a service task cannot do; use a script task", n.ID))
	}
	if !entities.KnownMultiInstanceType(n.MultiInstanceType) {
		v.errors = append(v.errors, fmt.Sprintf(
			"Flow node %s repeats %q, which the engine cannot run; a step repeats parallel, sequential or none", n.ID, n.MultiInstanceType))
	}
	if err := n.LoopCardinalityError(); err != nil {
		v.errors = append(v.errors, err.Error())
	}
	// The engine reads a timer from either place; see the event handler.
	for _, timer := range []string{n.GetStringProperty("timer_duration"), n.Condition} {
		if err := entities.CheckRepeatingTimer(timer); err != nil {
			v.errors = append(v.errors, fmt.Sprintf("Flow node %s: %v", n.ID, err))
		}
	}
	v.checkStartSchedule(n)
}

// checkStartSchedule refuses a schedule the engine could not keep, at deploy
// rather than at the first time it was due.
//
// A schedule starts the whole process, so it belongs on the process's own
// start step: on any other step, or on the start of a sub-process, nothing
// would ever read it and the process would sit unstarted with no sign why.
func (v *Visitor) checkStartSchedule(n *entities.Node) {
	for _, child := range n.Nodes {
		if entities.HasStartSchedule(child) {
			v.errors = append(v.errors, fmt.Sprintf(
				"Flow node %s has a schedule, but it is inside %s; only the process's own start step can start it on a schedule", child.ID, n.ID))
		}
	}
	if !entities.HasStartSchedule(n) {
		return
	}
	if n.Type != entities.StartEvent {
		v.errors = append(v.errors, fmt.Sprintf(
			"Flow node %s has a schedule, but only a start step can start a process on a schedule", n.ID))
		return
	}
	// The same rule for a model that names a step's parent instead of
	// nesting it: the worker reads only the start steps with no parent.
	if n.ParentID != "" {
		v.errors = append(v.errors, fmt.Sprintf(
			"Flow node %s has a schedule, but it is inside %s; only the process's own start step can start it on a schedule", n.ID, n.ParentID))
		return
	}
	if _, _, err := entities.StartScheduleOf(n); err != nil {
		v.errors = append(v.errors, fmt.Sprintf("Flow node %s: %v", n.ID, err))
		return
	}
	// One start step starts the process one way.
	for _, other := range []string{"message_name", "signal_name"} {
		if n.GetStringProperty(other) != "" {
			v.errors = append(v.errors, fmt.Sprintf(
				"Flow node %s has both a schedule and a %s; give each way of starting its own start step", n.ID, other))
		}
	}
}

func (v *Visitor) VisitSequenceFlow(sf *entities.SequenceFlow) {
	if sf.ID == "" {
		v.errors = append(v.errors, "Sequence flow ID is missing")
	}
	if sf.SourceRef == "" {
		v.errors = append(v.errors, fmt.Sprintf("Sequence flow %s has no source reference", sf.ID))
	}
	if sf.TargetRef == "" {
		v.errors = append(v.errors, fmt.Sprintf("Sequence flow %s has no target reference", sf.ID))
	}
}

func (v *Visitor) Errors() []string {
	return v.errors
}

func (v *Visitor) IsValid() bool {
	return len(v.errors) == 0
}
