package entities

// DecisionPointKind is the way a step of a process decides from the
// instance's variables: what it is, and so where its condition is written.
type DecisionPointKind string

const (
	// DecisionPointGateway is an exclusive or inclusive gateway choosing among
	// its outgoing flows by their conditions.
	DecisionPointGateway DecisionPointKind = "gateway"
	// DecisionPointConditionalEvent is a step that waits for a condition to
	// become true. Usually a conditional event, which gives the kind its name;
	// but the engine re-reads the condition of any step a token rests on, so
	// any step that carries one is a point of this kind.
	DecisionPointConditionalEvent DecisionPointKind = "conditional_event"
	// DecisionPointCompletionCondition is a repeating step, or an ad-hoc
	// sub-process, deciding whether it is done.
	DecisionPointCompletionCondition DecisionPointKind = "completion_condition"
	// DecisionPointDecisionTable is a decision table consulted by a business
	// rule task, or by a user task to decide who does it.
	DecisionPointDecisionTable DecisionPointKind = "decision_table"
	// DecisionPointCalledProcess is a call activity handing the instance's
	// variables to another process, which decides from them as it sees fit.
	// What that process reads is not known here, so a point of this kind is
	// never analysed.
	DecisionPointCalledProcess DecisionPointKind = "called_process"
	// DecisionPointCollection is a repeating step taking the list it repeats
	// over from a variable. With no list it runs once and says nothing.
	DecisionPointCollection DecisionPointKind = "collection"
)
