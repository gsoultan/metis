package impl

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The trace, built as the run happens.
//
// Two things write to it: the engine, through the event dispatcher this
// simulation gives it, and the simulation's own job service, which knows things
// the engine never announces — that a day passed, that a service was answered.
//
// Every step carries a *delta* rather than the whole variable map. A reader can
// then mark the values that moved, which is the question somebody scrubbing a
// trace is actually asking; re-printing forty variables per step answers it by
// making them find the difference themselves.
type simulationRecorder struct {
	mu    sync.Mutex
	clock *virtualClock

	steps    []entities.SimulationStep
	previous map[string]any
	tokens   []string

	incidents []entities.SimulationIncident

	// budget stops a runaway loop at a known number rather than at the
	// transaction timeout. Once it is spent every further step is dropped and
	// the run is reported as SimulationStepBudget.
	budget    int
	exhausted bool
}

func newSimulationRecorder(clock *virtualClock, budget int) *simulationRecorder {
	return &simulationRecorder{
		clock:    clock,
		previous: map[string]any{},
		budget:   budget,
	}
}

// OnEvent makes the recorder a ProcessObserver, so the engine's own lifecycle
// events become steps without the engine knowing a simulation is watching.
func (r *simulationRecorder) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	_ = ctx

	node := ""
	label := ""
	if event.Node != nil {
		node = event.Node.ID
		label = nodeLabel(event.Node)
	}

	kind, note := describeEvent(event, label)
	if kind == "" {
		return
	}

	var vars map[string]any
	if event.Instance != nil {
		vars = event.Instance.Variables
		r.setTokens(event.Instance)
	}
	if event.Variables != nil {
		vars = event.Variables
	}

	r.Record(entities.SimulationStep{Node: node, Event: kind, Note: note}, vars)
}

// Record appends one step, stamping it with virtual time and the variables that
// changed since the last one.
func (r *simulationRecorder) Record(step entities.SimulationStep, vars map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.steps) >= r.budget {
		r.exhausted = true
		return
	}

	step.Index = len(r.steps)
	step.Clock = r.clock.Now()
	if step.Tokens == nil {
		step.Tokens = append([]string(nil), r.tokens...)
	}
	step.VariablesDelta = r.deltaLocked(vars)

	if step.Incident != nil {
		r.incidents = append(r.incidents, *step.Incident)
	}

	r.steps = append(r.steps, step)
}

// RecordIncident notes a refusal and the step it happened at.
func (r *simulationRecorder) RecordIncident(node, message, hint string, vars map[string]any) {
	incident := &entities.SimulationIncident{Node: node, Message: message, Hint: hint}
	r.Record(entities.SimulationStep{
		Node:     node,
		Event:    "incident",
		Note:     message,
		Incident: incident,
	}, vars)
}

// deltaLocked reports which variables this step wrote. Caller holds the lock.
//
// A value written back identical still counts as written: the engine said it
// wrote it, and hiding a no-op assignment is unhelpful to the one person who is
// debugging exactly that.
func (r *simulationRecorder) deltaLocked(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}

	delta := map[string]any{}
	for name, value := range vars {
		old, existed := r.previous[name]
		if !existed || !sameValue(old, value) {
			delta[name] = value
		}
	}

	r.previous = maps.Clone(vars)
	if len(delta) == 0 {
		return nil
	}
	return delta
}

func (r *simulationRecorder) setTokens(instance *entities.ProcessInstance) {
	held := make([]string, 0, len(instance.Tokens))
	for _, token := range instance.Tokens {
		if token.Node != nil {
			held = append(held, token.Node.ID)
		}
	}
	r.mu.Lock()
	r.tokens = held
	r.mu.Unlock()
}

func (r *simulationRecorder) snapshot() ([]entities.SimulationStep, []entities.SimulationIncident, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]entities.SimulationStep(nil), r.steps...),
		append([]entities.SimulationIncident(nil), r.incidents...),
		r.exhausted
}

// sameValue compares two variable values well enough to decide whether to mark
// one as changed. Formatting rather than reflection: variables are JSON-shaped
// by the time they reach here, and this is a display decision, not a semantic
// one — being wrong marks a value as changed that was not, which costs a reader
// one glance.
func sameValue(a, b any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func nodeLabel(node *entities.Node) string {
	if node == nil {
		return ""
	}
	if node.Name != "" {
		return node.Name
	}
	return node.ID
}

// describeEvent turns an engine event into a step kind and a sentence.
//
// The sentence is written here, on the server, rather than in the browser, so
// every client — the designer, the SDK, a CI log — says the same thing about
// the same run. It is written for somebody who does not read node ids, which is
// the standing rule for anything user-facing in this system.
func describeEvent(event entities.ProcessEvent, label string) (kind string, note string) {
	switch event.Type {
	case entities.EventProcessStarted:
		name := "the process"
		if event.Instance != nil && event.Instance.Definition != nil && event.Instance.Definition.Name != "" {
			name = event.Instance.Definition.Name
		}
		return "started", fmt.Sprintf("Started %s", name)

	case entities.EventNodeReached:
		return "entered", fmt.Sprintf("Reached %s", label)

	case entities.EventTaskCreated:
		return "waiting", fmt.Sprintf("Waiting on %s", label)

	case entities.EventTaskClaimed:
		return "waiting", fmt.Sprintf("%s was picked up", label)

	case entities.EventTaskCompleted:
		return "left", fmt.Sprintf("%s was completed", label)

	case entities.EventTaskCanceled:
		return "left", fmt.Sprintf("%s was withdrawn", label)

	case entities.EventProcessCompleted:
		return "ended", "The process finished"

	default:
		// An event this recorder has no sentence for is dropped rather than
		// printed as its type name. A trace with `decision_evaluated` in it
		// reads like a log, and a log is the thing this replaces.
		return "", ""
	}
}
