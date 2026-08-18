package models

import "time"

// ServiceCallModel records one outbound call a service task makes.
//
// It exists because the call and the token advance cannot share a transaction.
// The call is network I/O and would hold a row lock across it; the advance is a
// read-modify-write on the instance and must be locked. So the call runs first,
// outside, and the advance commits after — and if that commit fails the job is
// retried and the call is made a second time. For an HTTP endpoint that charges
// a card, that is a second charge.
//
// This row is the memory between attempts: written before the call, completed
// with the response after it, and consulted by every retry. A retry that finds a
// completed row reuses its response instead of calling again.
type ServiceCallModel struct {
	Base

	// The call's identity. One service task, in one instance, on one iteration
	// of a multi-instance node, makes one call — so these three are unique
	// together, and that uniqueness is what makes a retry recognisable.
	//
	// 191 rather than 255 because these three make up a unique index and MySQL
	// bounds an index key by bytes, not characters.
	InstanceID  UUID   `gorm:"index;uniqueIndex:ux_service_calls_identity,priority:1" json:"instance_id,omitzero"`
	NodeID      string `gorm:"size:191;uniqueIndex:ux_service_calls_identity,priority:2" json:"node_id"`
	IterationID string `gorm:"size:191;uniqueIndex:ux_service_calls_identity,priority:3" json:"iteration_id,omitzero"`

	// ProjectID carries the tenant, so this table scopes like every other.
	ProjectID UUID `gorm:"index" json:"project_id,omitzero"`

	// IdempotencyKey is what the downstream sees, and it does not change
	// between attempts — that is the whole point. It is derived from the three
	// identity columns rather than generated, so it survives a restart that
	// loses whatever was in memory.
	IdempotencyKey string `gorm:"size:128;index" json:"idempotency_key"`

	// Status is ServiceCallInFlight or ServiceCallCompleted.
	Status string `gorm:"size:32" json:"status"`

	// Attempts counts how many times the call has been started. A number above
	// one means a previous attempt did not finish, which is the interesting
	// case for anyone reading this table during an incident.
	Attempts int `json:"attempts"`

	// Response is what the call returned, kept so a retry after a failed commit
	// can finish the work without repeating the call.
	Response map[string]any `gorm:"type:text;serializer:json" json:"response,omitzero"`

	CompletedAt *time.Time `json:"completed_at,omitzero"`
}

// TableName overrides the table name for ServiceCallModel.
func (ServiceCallModel) TableName() string {
	return "service_calls"
}

// The two states a recorded call can be in.
//
// There is deliberately no "failed": a call that errored is left in flight and
// retried, because a client cannot tell a request that never arrived from one
// whose response was lost. The idempotency key is what makes that safe — the
// downstream sees the same key twice and answers once.
const (
	ServiceCallInFlight  = "in_flight"
	ServiceCallCompleted = "completed"
)
