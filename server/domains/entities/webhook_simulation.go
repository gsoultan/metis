package entities

import (
	"time"

	"github.com/google/uuid"
)

// WebhookSimulation is a delivery somebody pasted in, to see what Metis would
// do with it if their partner sent it for real.
//
// The signature fields are optional. Without them the body is still read and
// correlated, which is most of what goes wrong while a sender is being set up.
type WebhookSimulation struct {
	Body       []byte
	Timestamp  string
	DeliveryID string
	Signature  string
}

// WebhookSimulationResult is what a real delivery like the simulated one would
// have done. Nothing it describes happened: no process moved, no delivery ID
// was recorded.
type WebhookSimulationResult struct {
	// WouldAct is true when a real delivery would reach at least one process,
	// as far as what was given shows. With no signature given it assumes one
	// that checks out, which Signature.Checked says.
	WouldAct bool `json:"would_act"`

	// Refusal is why a real delivery would be refused, in the words its sender
	// would be answered with. Empty when it would be accepted.
	Refusal string `json:"refusal,omitzero"`

	Enabled   bool                       `json:"enabled"`
	Signature WebhookSimulationSignature `json:"signature"`

	// BodyProblem is why the body could not be read, or the correlation rule
	// found nothing in it. When it is set nothing after it was looked at.
	BodyProblem string `json:"body_problem,omitzero"`

	MessageName           string `json:"message_name"`
	CorrelationExpression string `json:"correlation_expression,omitzero"`
	CorrelationKey        string `json:"correlation_key,omitzero"`

	// AlreadyReceived is true when a delivery with this ID has been acted on
	// recently: a real one would be answered as a duplicate and change nothing.
	AlreadyReceived bool `json:"already_received,omitzero"`

	// Waiting is the first few of the instances waiting for the message, and
	// WaitingTotal how many there are.
	Waiting      []WebhookSimulationWaiting `json:"waiting"`
	WaitingTotal int                        `json:"waiting_total"`

	// Starts is the processes a real delivery would start, which only an
	// uncorrelated webhook does.
	Starts []WebhookSimulationStart `json:"starts,omitzero"`
}

// WebhookSimulationSignature is what came of checking the signature, if one
// was given.
type WebhookSimulationSignature struct {
	Checked bool   `json:"checked"`
	Valid   bool   `json:"valid"`
	Problem string `json:"problem,omitzero"`
}

// WebhookSimulationWaiting is one instance a real delivery would move on.
type WebhookSimulationWaiting struct {
	InstanceID  uuid.UUID `json:"instance_id"`
	ProcessKey  string    `json:"process_key,omitzero"`
	ProcessName string    `json:"process_name,omitzero"`
	BusinessKey string    `json:"business_key,omitzero"`
	NodeID      string    `json:"node_id"`
	NodeName    string    `json:"node_name,omitzero"`
	Since       time.Time `json:"since"`
}

// WebhookSimulationStart is one process a real delivery would start.
type WebhookSimulationStart struct {
	ProcessKey  string `json:"process_key"`
	ProcessName string `json:"process_name,omitzero"`
	NodeName    string `json:"node_name,omitzero"`
}
