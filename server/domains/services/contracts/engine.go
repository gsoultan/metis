// Package contracts defines the narrow interfaces that components depend on.
// Following the Interface Segregation Principle (ISP), the ExecutionEngine is
// decomposed into four focused sub-interfaces.  Use the sub-interface that
// matches your dependency rather than the composite ExecutionEngine.
package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
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
	ListSubProcesses(ctx context.Context, parentInstanceID uuid.UUID) ([]entities.ProcessInstance, error)
	// GetRootInstance walks the parent chain and returns the top-level ancestor.
	GetRootInstance(ctx context.Context, instanceID uuid.UUID) (entities.ProcessInstance, error)
	GetExecutionPath(ctx context.Context, instanceID uuid.UUID) (entities.ExecutionPath, error)
	GetAuditLogs(ctx context.Context, instanceID uuid.UUID) ([]entities.AuditEntry, error)
}

// EngineEventBus handles process events, signals, messages, escalation, and compensation.
type EngineEventBus interface {
	DispatchEvent(ctx context.Context, event entities.ProcessEvent)
	BroadcastSignal(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error
	SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
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
