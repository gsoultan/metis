package entities

import (
	"time"

	"github.com/google/uuid"
)

// Simulation — running a definition on the real engine, against a virtual
// clock, with nothing persisted and nothing sent.
//
// The point of running the *real* engine is that the semantics people get wrong
// are the ones a reimplementation would get wrong too: gateway selection, token
// joins, boundary events, compensation, multi-instance. A second engine written
// for simulation would drift from this one, and a simulator that disagrees with
// production is worse than none — it certifies models that will not run.
//
// So a simulation differs from a real run in exactly three ways, each of them
// an injection point that already existed:
//
//  1. Its own event dispatcher, carrying only the recorder. No SSE fan-out, no
//     notifications, no webhook deliveries.
//  2. Its own job service. Timers move a virtual clock instead of sleeping, and
//     service tasks are answered from the request instead of calling a connector.
//  3. The whole run happens inside one UnitOfWork that is always rolled back.
//     Tokens, tasks, jobs, incidents and audit rows are written by the real code
//     and then discarded, so the audit trail stays evidentiary.
//
// Rolling back protects the database; it does not un-send an HTTP request. (2)
// is therefore not an optimisation — it is the only reason a simulation cannot
// charge a real card.

// SimulationOutcome is how a run ended.
type SimulationOutcome string

const (
	// SimulationOutcomeCompleted — the process reached an end event.
	SimulationOutcomeCompleted SimulationOutcome = "completed"
	// SimulationOutcomeNeedsAnswer — the run reached a step the outside world has to
	// answer for and nothing in the request answered it. Not an error: it is
	// the process asking a question, and it is the normal way a case is built.
	SimulationOutcomeNeedsAnswer SimulationOutcome = "needs_answer"
	// SimulationIncident — the engine refused. A gateway that matched nothing,
	// a missing default flow. This is the finding somebody ran a simulation for.
	SimulationOutcomeIncident SimulationOutcome = "incident"
	// SimulationOutcomeStepBudget — the run hit its step limit, which in practice means
	// a loop the author did not intend.
	SimulationOutcomeStepBudget SimulationOutcome = "step_budget"
	// SimulationOutcomeTimeout — the run took longer than a simulation is allowed to.
	SimulationOutcomeTimeout SimulationOutcome = "timeout"
)

// SimulationAnswerKind names what a step is being answered for.
type SimulationAnswerKind string

const (
	// AnswerPerson answers a user or manual task: who did it, and how long they took.
	AnswerPerson SimulationAnswerKind = "person"
	// AnswerService answers a service or send task: what it returned, or how it failed.
	AnswerService SimulationAnswerKind = "service"
	// AnswerMessage answers a waiting catch event: when the message arrived.
	AnswerMessage SimulationAnswerKind = "message"
)

// SimulationAnswer is what the outside world says when the process asks.
//
// Keyed by node rather than held in a list with its own identity, because that
// is how it is authored — you point at the step on the diagram and answer it —
// and because one step can only be answered once.
type SimulationAnswer struct {
	Node string               `json:"node"`
	Kind SimulationAnswerKind `json:"kind"`

	// Actor and After answer a person's task. After is ISO-8601 ("PT24H"),
	// which is what BPMN uses and what the virtual clock advances by.
	Actor string `json:"actor,omitempty"`
	After string `json:"after,omitempty"`

	// Returns is what a service task hands back, merged into the instance
	// variables exactly as a worker's completion would.
	Returns map[string]any `json:"returns,omitempty"`
	// Fails is an error code instead of a result, so a boundary error event can
	// be exercised without anything actually failing.
	Fails string `json:"fails,omitempty"`

	// ArrivesAfter answers a waiting catch event. ISO-8601, like After.
	ArrivesAfter string `json:"arrives_after,omitempty"`
}

// SimulationDecision records what a gateway or decision table decided.
//
// A gateway that selected nothing leaves Chose empty and raises an incident; it
// never picks a branch on its own, in a simulation for the same reason it never
// does in production.
type SimulationDecision struct {
	Expression string `json:"expression"`
	Result     string `json:"result"`
	Chose      string `json:"chose,omitempty"`
	Rule       string `json:"rule,omitempty"`
}

// SimulationIncident is a refusal the engine raised during the run.
type SimulationIncident struct {
	Node    string `json:"node"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// SimulationStep is one thing that happened, stamped with virtual time.
type SimulationStep struct {
	Index  int       `json:"i"`
	Clock  time.Time `json:"clock"`
	Node   string    `json:"node"`
	Event  string    `json:"event"`
	Tokens []string  `json:"tokens"`
	Flow   string    `json:"flow,omitempty"`

	// VariablesDelta is only what this step changed, so a reader can mark the
	// values that moved rather than re-printing the whole map per step.
	VariablesDelta map[string]any `json:"variables_delta,omitempty"`

	// Note is the step in business words — "Marc submitted a £4,200 expense",
	// never "Task_Started node=Activity_1x2y". Written here rather than in the
	// browser so every client says the same thing.
	Note string `json:"note"`

	Decision *SimulationDecision `json:"decision,omitempty"`
	Incident *SimulationIncident `json:"incident,omitempty"`
}

// SimulationRequest is one run.
type SimulationRequest struct {
	ProjectID uuid.UUID `json:"project_id"`

	// DefinitionKey with Version pins what runs. Version 0 means whatever is
	// live, which is what a caller who does not care sends.
	DefinitionKey string `json:"definition_key"`
	Version       int    `json:"version"`

	Variables map[string]any     `json:"variables"`
	Answers   []SimulationAnswer `json:"answers"`

	// ClockStart is where virtual time begins. Zero means now, so a caller who
	// does not care gets a readable trace; a caller writing a CI assertion sets
	// it, because a trace that moves with the wall clock cannot be asserted on.
	ClockStart time.Time `json:"clock_start,omitzero"`

	// Seed fixes every estimate. Same seed, same definition version, same
	// answers — same trace, which is what makes a simulation assertable.
	Seed int64 `json:"seed"`

	// MaxSteps stops a runaway loop at a known number instead of at the
	// transaction timeout, turning a hang into a finding.
	MaxSteps int `json:"max_steps"`
}

// SimulationRun is the whole trace, returned in one response.
//
// All of it at once, deliberately: stepping, scrubbing and stepping *back* are
// then array indexing in the client rather than a request per step, and an SDK
// caller gets everything an assertion needs from one call.
type SimulationRun struct {
	RunID      uuid.UUID `json:"run_id"`
	Definition struct {
		Key     string    `json:"key"`
		Version int       `json:"version"`
		ID      uuid.UUID `json:"id"`
	} `json:"definition"`

	Outcome     SimulationOutcome `json:"outcome"`
	EndedAtNode string            `json:"ended_at_node,omitempty"`

	// AwaitingNode and AwaitingLabel are set when Outcome is
	// SimulationOutcomeNeedsAnswer: the step the run stopped to ask about, and what the
	// model calls it, so the question can be asked in the author's own words.
	AwaitingNode  string `json:"awaiting_node,omitempty"`
	AwaitingLabel string `json:"awaiting_label,omitempty"`

	VirtualDurationMs int64                `json:"virtual_duration_ms"`
	Variables         map[string]any       `json:"variables"`
	Incidents         []SimulationIncident `json:"incidents"`
	Steps             []SimulationStep     `json:"steps"`
}

// DefaultSimulationMaxSteps bounds a run that would otherwise loop forever.
const DefaultSimulationMaxSteps = 500

// MaxSimulationSteps is the ceiling a caller cannot raise past.
//
// The run holds a database transaction open for its whole length, on a pool
// that real process instances are also using. A caller asking for a hundred
// thousand steps is asking to starve production of connections.
const MaxSimulationSteps = 5000

// Normalize fills in the defaults a caller who does not care can omit, and
// clamps the ones that would cost somebody else their connection.
func (r *SimulationRequest) Normalize(now time.Time) {
	if r.MaxSteps <= 0 {
		r.MaxSteps = DefaultSimulationMaxSteps
	}
	if r.MaxSteps > MaxSimulationSteps {
		r.MaxSteps = MaxSimulationSteps
	}
	if r.ClockStart.IsZero() {
		r.ClockStart = now
	}
	if r.Variables == nil {
		r.Variables = map[string]any{}
	}
}

// AnswerFor returns the answer for a node, and whether there was one.
func (r *SimulationRequest) AnswerFor(nodeID string) (SimulationAnswer, bool) {
	for _, answer := range r.Answers {
		if answer.Node == nodeID {
			return answer, true
		}
	}
	return SimulationAnswer{}, false
}
