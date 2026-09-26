package entities

// ProcessEvent represents an event in the process lifecycle.
type ProcessEvent struct {
	Type      string           `json:"type"`
	Instance  *ProcessInstance `json:"instance,omitzero"`
	Project   *Project         `json:"project,omitzero"`
	Node      *Node            `json:"node,omitzero"`
	Timestamp int64            `json:"timestamp"`
	Variables map[string]any   `json:"variables,omitzero"`
	// Assignee is the person this event is about, when it is about one.
	//
	// It exists because who an event concerns was being smuggled through
	// Variables, which is the *instance's* business data — the amount on a
	// quotation, the customer's name. Two events put an "assignee" key there by
	// hand and the rest did not, so an observer reading it found the right
	// answer on those two and nothing on the others.
	//
	// Additive on purpose: an empty value means the event is not about a
	// particular person, which is what every dispatch that does not set it
	// means. Nothing that consumes Variables changes.
	Assignee string `json:"assignee,omitzero"`
	// Audited says the code that raised this event wrote its audit entry
	// itself, so the audit observer must not write a second one.
	//
	// The task service records each task action with who performed it, which
	// the event does not carry, and raised the event as well — so every claim,
	// release and completion was in the trail twice. Not sent to browsers: it is
	// about the trail, not about what happened.
	Audited bool `json:"-"`
}

const (
	EventProcessStarted = "ProcessStarted"
	EventNodeReached    = "NodeReached"
	EventTaskCreated    = "TaskCreated"
	EventTaskCompleted  = "TaskCompleted"
	EventTaskUpdated    = "TaskUpdated"
	EventTaskClaimed    = "TaskClaimed"
	// EventTaskCanceled is raised when an activity is interrupted and the task it
	// created is withdrawn, so the audit trail says why it left the inbox.
	EventTaskCanceled     = "TaskCanceled"
	EventProcessCompleted = "ProcessCompleted"
)
