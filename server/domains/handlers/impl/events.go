package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts2 "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// --- Minimal engine surfaces used by event handlers ---
//
// Each interface is the narrowest set of methods the handler actually calls,
// satisfying the Interface Segregation Principle (ARCH-2).

// eventBusRunner is the surface needed by throw-event handlers that must
// broadcast a signal/message AND then advance the token.
type eventBusRunner interface {
	contracts2.EngineRunner
	BroadcastSignal(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error
	SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
}

// endEventEngine is the surface needed by EndEventHandler: it reads the parent
// process state, updates it after sub-process completion, and dispatches completion events.
type endEventEngine interface {
	contracts2.EngineRunner
	contracts2.EngineEventBus
	GetInstance(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
	GetProcessDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
}

// terminateEventEngine is the surface needed by TerminateEndEventHandler:
// update the instance and dispatch a completion event.
type terminateEventEngine interface {
	contracts2.EngineRunner
	DispatchEvent(ctx context.Context, event entities.ProcessEvent)
}

// StartEventHandler handles the start of a process.
type StartEventHandler struct {
	engine contracts2.EngineRunner
}

func (h *StartEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// EndEventHandler handles the end of a process path.
type EndEventHandler struct {
	engine endEventEngine
}

func (h *EndEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	instance.RemoveTokenByNode(&node)

	// If node belongs to a sub-process, check if there are other tokens in the same sub-process.
	if node.ParentID != "" {
		hasMoreTokensInSubProcess := false
		for _, t := range instance.Tokens {
			if tn := def.FindNode(t.Node.ID); tn != nil && tn.ParentID == node.ParentID {
				hasMoreTokensInSubProcess = true
				break
			}
		}

		if !hasMoreTokensInSubProcess {
			// Sub-process completed, proceed from the sub-process node.
			subProcessNode := def.FindNode(node.ParentID)
			if subProcessNode != nil {
				return h.engine.ProceedIteration(ctx, instance, def, subProcessNode.ID, iterationID)
			}
		}
		return h.engine.UpdateInstance(ctx, *instance)
	}

	if len(instance.Tokens) == 0 {
		instance.Status = entities.ProcessCompleted
		h.engine.DispatchEvent(ctx, entities.ProcessEvent{
			Type:      entities.EventProcessCompleted,
			Instance:  instance,
			Project:   instance.Project,
			Node:      &node,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
		})

		// Persist this instance's completion before handing control back to the
		// parent. If the resume then fails, the error surfaces with the
		// sub-process already recorded as completed, rather than the completion
		// being lost along with it.
		if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
			return fmt.Errorf("persist completion of instance %s: %w", instance.ID, err)
		}

		if instance.ParentInstance != nil {
			return h.resumeParent(ctx, instance)
		}
		return nil
	}
	return h.engine.UpdateInstance(ctx, *instance)
}

// resumeParent hands control back to the call activity in the parent process
// once a sub-process instance has completed.
//
// Every failure here is returned. A parent that is not resumed waits at its call
// activity forever, and the sub-process it was waiting on has already been
// marked completed — so an error swallowed here is a process that can never
// finish and never reports why.
func (h *EndEventHandler) resumeParent(ctx context.Context, instance *entities.ProcessInstance) error {
	parentInstance, err := h.engine.GetInstance(ctx, instance.ParentInstance.ID)
	if err != nil {
		return fmt.Errorf("load parent instance %s of sub-process %s: %w", instance.ParentInstance.ID, instance.ID, err)
	}
	if parentInstance.Definition == nil {
		return fmt.Errorf("parent instance %s has no definition reference", parentInstance.ID)
	}

	parentDef, err := h.engine.GetProcessDefinition(ctx, parentInstance.Definition.ID)
	if err != nil {
		return fmt.Errorf("load definition %s of parent instance %s: %w", parentInstance.Definition.ID, parentInstance.ID, err)
	}

	parentNodeID := ""
	if instance.ParentNode != nil {
		parentNodeID = instance.ParentNode.ID
	}
	if callActivityNode := parentDef.FindNode(parentNodeID); callActivityNode != nil {
		applyOutputMapping(callActivityNode, instance, &parentInstance)
	}

	if err := h.engine.UpdateInstance(ctx, parentInstance); err != nil {
		return fmt.Errorf("update parent instance %s after sub-process %s completed: %w", parentInstance.ID, instance.ID, err)
	}
	if err := h.engine.ProceedIteration(ctx, &parentInstance, parentDef, parentNodeID, ""); err != nil {
		return fmt.Errorf("resume parent instance %s at call activity %q: %w", parentInstance.ID, parentNodeID, err)
	}
	return nil
}

// applyOutputMapping copies variables from a finished sub-process back into its
// parent, honouring the call activity's out_mapping when one is configured and
// copying every variable otherwise.
func applyOutputMapping(callActivityNode *entities.Node, child *entities.ProcessInstance, parent *entities.ProcessInstance) {
	if mapping, ok := callActivityNode.Properties["out_mapping"].(map[string]any); ok && len(mapping) > 0 {
		for target, source := range mapping {
			if srcKey, ok := source.(string); ok {
				if val, ok := child.Variables[srcKey]; ok {
					parent.SetVariable(target, val)
				}
			}
		}
		return
	}
	for k, v := range child.Variables {
		parent.SetVariable(k, v)
	}
}

// TerminateEndEventHandler handles the termination of all paths in the process.
type TerminateEndEventHandler struct {
	engine terminateEventEngine
}

func (h *TerminateEndEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	instance.Tokens = nil // Remove all tokens
	instance.Status = entities.ProcessCompleted
	h.engine.DispatchEvent(ctx, entities.ProcessEvent{
		Type:      entities.EventProcessCompleted,
		Instance:  instance,
		Project:   instance.Project,
		Node:      &node,
		Timestamp: time.Now().Unix(),
		Variables: instance.Variables,
	})
	return h.engine.UpdateInstance(ctx, *instance)
}

// IntermediateCatchEventHandler handles events that catch information, such as timers.
type IntermediateCatchEventHandler struct {
	jobService contracts2.JobService
	subRepo    contracts.SubscriptionRepository
}

func (h *IntermediateCatchEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	if signalName := node.GetStringProperty("signal_name"); signalName != "" {
		return h.subRepo.Create(ctx, h.subToModel(entities.NewSignalSubscription(instance.Project, instance, &node, signalName)))
	}

	if messageName := node.GetStringProperty("message_name"); messageName != "" {
		correlationKey, err := entities.ResolveCorrelationKey(node.GetStringProperty("correlation_key"), instance.Variables)
		if err != nil {
			return fmt.Errorf("resolve correlation key for message %q on node %s: %w", messageName, node.ID, err)
		}
		return h.subRepo.Create(ctx, h.subToModel(entities.NewMessageSubscription(instance.Project, instance, &node, messageName, correlationKey)))
	}

	if duration := node.GetStringProperty("timer_duration"); duration != "" {
		return h.jobService.EnqueueTimer(ctx, *instance, node, duration)
	}

	if node.Condition != "" {
		// Asynchronous timer execution via job service.
		return h.jobService.EnqueueTimer(ctx, *instance, node, node.Condition)
	}
	// If no condition, it's a passthrough for now (or a generic catch event)
	return nil
}

func (h *IntermediateCatchEventHandler) subToModel(ent entities.EventSubscription) models.Subscription {
	var projectID, instanceID uuid.UUID
	if ent.Project != nil {
		projectID = ent.Project.ID
	}
	if ent.Instance != nil {
		instanceID = ent.Instance.ID
	}
	return models.Subscription{
		Base: models.Base{
			ID:        models.UUID(ent.ID),
			CreatedAt: ent.CreatedAt,
		},
		ProjectID:  models.UUID(projectID),
		InstanceID: models.UUID(instanceID),
		NodeID: func() string {
			if ent.Node != nil {
				return ent.Node.ID
			}
			return ""
		}(),
		Type:           models.SubscriptionType(ent.Type),
		EventName:      ent.EventName,
		CorrelationKey: ent.CorrelationKey,
	}
}

// SignalThrowEventHandler broadcasts a signal and continues.
type SignalThrowEventHandler struct {
	engine eventBusRunner
}

func (h *SignalThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	signalName := node.GetStringProperty("signal_name")
	if signalName != "" {
		// A broadcast that failed must not be reported as thrown: the catching
		// instances stay parked forever and this one would advance past the throw
		// as though they had been notified.
		if err := h.engine.BroadcastSignal(ctx, instance.Project.ID, signalName, instance.Variables); err != nil {
			return fmt.Errorf("broadcast signal %q from node %s: %w", signalName, node.ID, err)
		}
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// IntermediateThrowEventHandler handles signals and messages in throw events.
type IntermediateThrowEventHandler struct {
	engine eventBusRunner
}

func (h *IntermediateThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	if signalName := node.GetStringProperty("signal_name"); signalName != "" {
		if err := h.engine.BroadcastSignal(ctx, instance.Project.ID, signalName, instance.Variables); err != nil {
			return fmt.Errorf("broadcast signal %q from node %s: %w", signalName, node.ID, err)
		}
	}
	if messageName := node.GetStringProperty("message_name"); messageName != "" {
		correlationKey, err := entities.ResolveCorrelationKey(node.GetStringProperty("correlation_key"), instance.Variables)
		if err != nil {
			return fmt.Errorf("resolve correlation key for message %q on node %s: %w", messageName, node.ID, err)
		}
		if err := h.engine.SendMessage(ctx, instance.Project.ID, messageName, correlationKey, instance.Variables); err != nil {
			return fmt.Errorf("send message %q from node %s: %w", messageName, node.ID, err)
		}
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// MessageThrowEventHandler sends a message and continues.
type MessageThrowEventHandler struct {
	engine eventBusRunner
}

func (h *MessageThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	messageName := node.GetStringProperty("message_name")
	if messageName != "" {
		correlationKey, err := entities.ResolveCorrelationKey(node.GetStringProperty("correlation_key"), instance.Variables)
		if err != nil {
			return fmt.Errorf("resolve correlation key for message %q on node %s: %w", messageName, node.ID, err)
		}
		if err := h.engine.SendMessage(ctx, instance.Project.ID, messageName, correlationKey, instance.Variables); err != nil {
			return fmt.Errorf("send message %q from node %s: %w", messageName, node.ID, err)
		}
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// ErrorEndEventHandler does not use engine — it simply returns a typed error for
// boundary-event matching.  The engine field has been removed to satisfy ISP.
type ErrorEndEventHandler struct{}

func (h *ErrorEndEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	errorCode := node.GetStringProperty("error_code")
	if errorCode == "" {
		errorCode = "unspecified"
	}
	// Removing token as it's an end event
	instance.RemoveTokenByNode(&node)
	// Returning error with code to be caught by boundary events
	return fmt.Errorf("BPMN_ERROR:%s", errorCode)
}

// EscalationThrowEventHandler handles throwing an escalation event.
type EscalationThrowEventHandler struct {
	engine contracts2.EngineEventBus
}

func (h *EscalationThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, escalationCode string) error {
	escalationCodeValue := node.GetStringProperty("escalation_code")
	return h.engine.TriggerEscalation(ctx, instance, def, node, escalationCodeValue)
}

// CompensationThrowEventHandler handles triggering compensation.
type CompensationThrowEventHandler struct {
	engine contracts2.EngineEventBus
}

func (h *CompensationThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	activityRef := node.GetStringProperty("activity_ref")
	return h.engine.TriggerCompensation(ctx, instance, def, node, activityRef)
}
