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

		// Notify and resume parent process if it exists
		if instance.ParentInstance != nil {
			parentInstance, err := h.engine.GetInstance(ctx, instance.ParentInstance.ID)
			if err == nil {
				// Resuming parent at the Call Activity node
				parentDef, err := h.engine.GetProcessDefinition(ctx, parentInstance.Definition.ID)
				if err == nil {
					parentNodeID := ""
					if instance.ParentNode != nil {
						parentNodeID = instance.ParentNode.ID
					}
					callActivityNode := parentDef.FindNode(parentNodeID)
					if callActivityNode != nil {
						// Apply output mapping from sub-process back to parent
						if mapping, ok := callActivityNode.Properties["out_mapping"].(map[string]any); ok && len(mapping) > 0 {
							for target, source := range mapping {
								if srcKey, ok := source.(string); ok {
									if val, ok := instance.Variables[srcKey]; ok {
										parentInstance.SetVariable(target, val)
									}
								}
							}
						} else {
							// Default: copy all variables back to parent
							for k, v := range instance.Variables {
								parentInstance.SetVariable(k, v)
							}
						}
					}

					h.engine.UpdateInstance(ctx, parentInstance)
					h.engine.ProceedIteration(ctx, &parentInstance, parentDef, parentNodeID, "")
				}
			}
		}
	}
	return h.engine.UpdateInstance(ctx, *instance)
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
		correlationKey := node.GetStringProperty("correlation_key") // Evaluation logic could be added here
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
			ID:        ent.ID,
			CreatedAt: ent.CreatedAt,
		},
		ProjectID:  projectID,
		InstanceID: instanceID,
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
		h.engine.BroadcastSignal(ctx, instance.Project.ID, signalName, instance.Variables)
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// IntermediateThrowEventHandler handles signals and messages in throw events.
type IntermediateThrowEventHandler struct {
	engine eventBusRunner
}

func (h *IntermediateThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	if signalName := node.GetStringProperty("signal_name"); signalName != "" {
		h.engine.BroadcastSignal(ctx, instance.Project.ID, signalName, instance.Variables)
	}
	if messageName := node.GetStringProperty("message_name"); messageName != "" {
		correlationKey := node.GetStringProperty("correlation_key")
		h.engine.SendMessage(ctx, instance.Project.ID, messageName, correlationKey, instance.Variables)
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// MessageThrowEventHandler sends a message and continues.
type MessageThrowEventHandler struct {
	engine eventBusRunner
}

func (h *MessageThrowEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	messageName := node.GetStringProperty("message_name")
	correlationKey := node.GetStringProperty("correlation_key")
	if messageName != "" {
		h.engine.SendMessage(ctx, instance.Project.ID, messageName, correlationKey, instance.Variables)
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
