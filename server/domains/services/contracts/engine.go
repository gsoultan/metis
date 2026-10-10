// Package contracts defines the narrow interfaces that components depend on.
// Following the Interface Segregation Principle (ISP), the ExecutionEngine is
// decomposed into four focused sub-interfaces.  Use the sub-interface that
// matches your dependency rather than the composite ExecutionEngine.
package contracts

import (
	"context"
	"errors"

	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// EngineRunner advances process instances through the BPMN graph.
type EngineRunner interface {
	StartProcess(ctx context.Context, projectID uuid.UUID, definitionKey string, vars map[string]any) (uuid.UUID, error)
	StartSubProcess(ctx context.Context, projectID uuid.UUID, definitionKey string, version int, vars map[string]any, parentInstanceID uuid.UUID, parentNodeID string) (uuid.UUID, error)
	ExecuteNode(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error
	ExecuteNodeIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error
	Proceed(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error
	ProceedIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error
	UpdateInstance(ctx context.Context, instance entities.ProcessInstance) error
}

// EngineReader provides read-only access to process state.
type EngineReader interface {
	GetInstance(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
	GetInstanceForUpdate(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
	GetProcessDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	ListInstances(ctx context.Context, projectID uuid.UUID) ([]entities.ProcessInstance, error)

	// ListInstancesPaged returns one window plus the total. A busy engine
	// produces instances continuously, so this is the list most likely to grow
	// past what a browser can hold.
	//
	// The filter is applied in the database. Narrowing the window after it came
	// back would mean a project's failures are findable only if they happen to
	// be among the newest twenty-five.
	ListInstancesPaged(ctx context.Context, projectID uuid.UUID, filter repocontracts.InstanceFilter, page repocontracts.Pagination) (repocontracts.Page[entities.ProcessInstance], error)

	// CountInstancesByStatus reports how many instances the project holds in
	// each state, so a page of rows can say what the rest of the project looks
	// like and offer the filter that reaches it.
	CountInstancesByStatus(ctx context.Context, projectID uuid.UUID, filter repocontracts.InstanceFilter) (map[models.ProcessStatus]int64, error)

	// InstanceAttention reports which instances are waiting on a person.
	//
	// Separate from status because status does not say it: a job that exhausts
	// its retries raises an incident and leaves the instance `active`. Nothing
	// writes models.ProcessFailed, so "is anything broken?" is a question about
	// open incidents and never about the status column.
	InstanceAttention(ctx context.Context, projectID uuid.UUID, filter repocontracts.InstanceFilter, onPage []uuid.UUID) (entities.InstanceAttention, error)
	ListSubProcesses(ctx context.Context, parentInstanceID uuid.UUID) ([]entities.ProcessInstance, error)
	GetExecutionPath(ctx context.Context, instanceID uuid.UUID) (entities.ExecutionPath, error)
	GetAuditLogs(ctx context.Context, instanceID uuid.UUID) ([]entities.AuditEntry, error)

	// ExportOCEL reads a project's audit trail as an OCEL 2.0 object-centric
	// event log, so the history this engine already records can be mined by the
	// tools that exist rather than only read in this application's timeline.
	ExportOCEL(ctx context.Context, projectID uuid.UUID, opts entities.OCELOptions) (entities.OCELLog, error)
}

// ErrMessagePartlyDelivered is wrapped into the error of a message send that
// failed for some of its recipients after others had already committed.
//
// Each recipient of a message — a waiting instance, a process it starts —
// commits on its own, so a send that fails part-way has still done some of its
// work. Sending the message again would do that work twice: a second start of a
// process that already started. A caller that retries must not retry this one.
var ErrMessagePartlyDelivered = errors.New("message reached some of its recipients before the rest failed")

// EngineEventBus handles process events, signals, messages, escalation, and compensation.
type EngineEventBus interface {
	DispatchEvent(ctx context.Context, event entities.ProcessEvent)
	BroadcastSignal(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error
	// SendMessage delivers a message to the instances waiting on it — every
	// one of them when correlationKey is empty — and, when it is empty, starts
	// the processes whose message start event listens for it. Its error wraps
	// ErrMessagePartlyDelivered when some of that work committed.
	SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
	// StartFromMessage starts the processes whose message start event listens
	// for messageName, and nothing else: no waiting instance is reached. It
	// reports how many started; its error wraps ErrMessagePartlyDelivered when
	// some did before another failed.
	StartFromMessage(ctx context.Context, projectID uuid.UUID, messageName string, vars map[string]any) (int, error)
	TriggerEscalation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, escalationCode string) error
	TriggerCompensation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, activityRef string) error
}

// ScriptExecutor evaluates embedded process scripts.
type ScriptExecutor interface {
	ExecuteScript(ctx context.Context, script string, scriptFormat string, variables map[string]any) (map[string]any, error)
}

// ExecutionEngine is the composition root that combines all engine sub-interfaces.
// Prefer declaring the narrower sub-interface (EngineRunner, EngineReader, etc.)
// in each dependency to follow the Interface Segregation Principle.
// Set* wiring methods have been removed; use serviceimpl.EngineOption instead.
type ExecutionEngine interface {
	EngineRunner
	EngineReader
	EngineEventBus
	ScriptExecutor
}
