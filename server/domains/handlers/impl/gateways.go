package impl

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/logic"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/rs/zerolog/log"
)

// parallelJoinEngine is the minimal engine surface needed by ParallelGatewayHandler.
// It needs GetInstanceForUpdate (EngineReader) for the atomic join check and the
// EngineRunner methods to update and advance the instance.
type parallelJoinEngine interface {
	servicecontracts.EngineRunner
	GetInstanceForUpdate(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
}

// ExclusiveGatewayHandler handles exclusive decision points where only one path is followed.
type ExclusiveGatewayHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *ExclusiveGatewayHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	flows := def.GetOutgoingFlows(node.ID)
	if len(flows) == 0 {
		return fmt.Errorf("exclusive gateway %s has no outgoing flows", node.ID)
	}

	var selectedFlow *entities.SequenceFlow
	evaluator := logic.GetConditionEvaluatorChain()
	for _, flow := range flows {
		if evaluator.Evaluate(flow.Condition, instance.Variables) {
			selectedFlow = flow
			break
		}
	}

	if selectedFlow == nil && node.DefaultFlow != "" {
		for _, flow := range flows {
			if flow.ID == node.DefaultFlow {
				selectedFlow = flow
				break
			}
		}
	}

	if selectedFlow == nil {
		if !allowImplicitDefaultFlow() {
			return fmt.Errorf(
				"BPMN_ERROR:no outgoing sequence flow could be selected at exclusive gateway %q: "+
					"no condition evaluated true and no default flow is declared", node.ID)
		}
		// Legacy behaviour, retained only behind GOBPM_ALLOW_IMPLICIT_DEFAULT_FLOW.
		selectedFlow = flows[0]
		logImplicitDefaultFlow(node.ID, selectedFlow.ID)
	}

	instance.RemoveTokenByNode(&node)
	targetNode := def.FindNode(selectedFlow.TargetRef)
	if targetNode != nil {
		instance.AddToken(targetNode)
	}

	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return err
	}

	return h.engine.ExecuteNode(ctx, instance, def, selectedFlow.TargetRef)
}

// ParallelGatewayHandler handles splitting and joining of parallel execution paths.
type ParallelGatewayHandler struct {
	engine parallelJoinEngine
}

func (h *ParallelGatewayHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	incoming := def.GetIncomingFlows(node.ID)
	outgoing := def.GetOutgoingFlows(node.ID)

	if len(incoming) > 1 {
		allArrived, err := h.recordAndCheckJoin(ctx, instance, node.ID, len(incoming))
		if err != nil {
			return err
		}
		if !allArrived {
			return nil // wait for remaining branches
		}
	}

	return h.fork(ctx, instance, def, &node, outgoing)
}

// recordAndCheckJoin atomically increments the join counter for this gateway using
// a SELECT FOR UPDATE lock on the instance row, preventing the race condition
// where two concurrent job goroutines read the same stale count.
// Returns true when all expected branches have arrived.
func (h *ParallelGatewayHandler) recordAndCheckJoin(ctx context.Context, instance *entities.ProcessInstance, nodeID string, expected int) (bool, error) {
	// Re-read the instance under a row-level lock so concurrent workers see each
	// other's updates before incrementing.
	fresh, err := h.engine.GetInstanceForUpdate(ctx, instance.ID)
	if err != nil {
		return false, fmt.Errorf("parallel gateway join: load instance for update: %w", err)
	}

	count := fresh.RecordJoinArrival(nodeID)

	if err := h.engine.UpdateInstance(ctx, fresh); err != nil {
		return false, err
	}

	// Sync caller's view so subsequent code uses fresh state.
	*instance = fresh

	if count < expected {
		return false, nil
	}

	// All branches arrived – clean up the join counter.
	instance.ClearJoin(nodeID)
	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return false, err
	}
	return true, nil
}

// fork removes the current token and spawns one token per outgoing flow.
func (h *ParallelGatewayHandler) fork(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, outgoing []*entities.SequenceFlow) error {
	instance.RemoveTokenByNode(node)
	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return err
	}
	for _, flow := range outgoing {
		targetNode := def.FindNode(flow.TargetRef)
		if targetNode != nil {
			instance.AddToken(targetNode)
		}
		if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		if err := h.engine.ExecuteNode(ctx, instance, def, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

// InclusiveGatewayHandler handles decision points where one or more paths can be followed.
type InclusiveGatewayHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *InclusiveGatewayHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	incoming := def.GetIncomingFlows(node.ID)
	outgoing := def.GetOutgoingFlows(node.ID)

	// Join: wait if any live token can still reach this gateway.
	if len(incoming) > 1 && h.hasLiveUpstreamToken(instance, def, node.ID) {
		return h.engine.UpdateInstance(ctx, *instance)
	}

	// Fork: evaluate all outgoing conditions using the shared evaluator chain.
	evaluator := logic.GetConditionEvaluatorChain()
	var selectedFlows []*entities.SequenceFlow
	for _, flow := range outgoing {
		if evaluator.Evaluate(flow.Condition, instance.Variables) {
			selectedFlows = append(selectedFlows, flow)
		}
	}

	// Fallback to the declared default flow.
	if len(selectedFlows) == 0 && node.DefaultFlow != "" {
		for _, flow := range outgoing {
			if flow.ID == node.DefaultFlow {
				selectedFlows = append(selectedFlows, flow)
				break
			}
		}
	}
	if len(selectedFlows) == 0 && len(outgoing) > 0 {
		if !allowImplicitDefaultFlow() {
			return fmt.Errorf(
				"BPMN_ERROR:no outgoing sequence flow could be selected at inclusive gateway %q: "+
					"no condition evaluated true and no default flow is declared", node.ID)
		}
		selectedFlows = append(selectedFlows, outgoing[0])
		logImplicitDefaultFlow(node.ID, outgoing[0].ID)
	}

	instance.RemoveTokenByNode(&node)
	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return err
	}
	for _, flow := range selectedFlows {
		targetNode := def.FindNode(flow.TargetRef)
		if targetNode != nil {
			instance.AddToken(targetNode)
		}
		if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		if err := h.engine.ExecuteNode(ctx, instance, def, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

// hasLiveUpstreamToken returns true if any active token (excluding those already
// sitting on gatewayID) can still reach the gateway.
//
// BuildAncestorSet constructs the reachability map once — O(nodes + flows) — so
// each token check is an O(1) map lookup rather than a full BFS per token.
func (h *InclusiveGatewayHandler) hasLiveUpstreamToken(instance *entities.ProcessInstance, def *entities.ProcessDefinition, gatewayID string) bool {
	reachable := def.BuildAncestorSet(gatewayID)
	for _, token := range instance.Tokens {
		if token.Node != nil && token.Node.ID != gatewayID && reachable[token.Node.ID] {
			return true
		}
	}
	return false
}

// EventBasedGatewayHandler handles gateways where execution waits for events.
type EventBasedGatewayHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *EventBasedGatewayHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	outgoing := def.GetOutgoingFlows(node.ID)

	instance.RemoveTokenByNode(&node)
	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return err
	}

	for _, flow := range outgoing {
		targetNode := def.FindNode(flow.TargetRef)
		if targetNode != nil {
			instance.AddToken(targetNode)
		}
		if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		if err := h.engine.ExecuteNode(ctx, instance, def, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

// envAllowImplicitDefaultFlow re-enables the pre-existing behaviour in which a
// gateway with no matching condition and no declared default flow silently took
// its first outgoing flow.
//
// That behaviour is a business-correctness hazard: a typo in a condition, or a
// variable absent at runtime, routed the instance down whichever branch the
// definition happened to list first — potentially "approve" instead of
// "reject" — and recorded it in the audit trail as a normal transition. BPMN
// 2.0 requires an exception when no outgoing flow can be selected.
//
// It is a behaviour change for existing definitions, so the old path remains
// available for one migration window. Deployments that set this should use the
// logged warnings to find and fix the affected definitions.
const envAllowImplicitDefaultFlow = "GOBPM_ALLOW_IMPLICIT_DEFAULT_FLOW"

func allowImplicitDefaultFlow() bool {
	v, err := strconv.ParseBool(os.Getenv(envAllowImplicitDefaultFlow))
	return err == nil && v
}

func logImplicitDefaultFlow(nodeID, flowID string) {
	log.Warn().
		Str("nodeId", nodeID).
		Str("flowId", flowID).
		Str("env", envAllowImplicitDefaultFlow).
		Msg("gateway had no matching condition and no default flow; took the first outgoing flow. " +
			"Declare an explicit default flow — this fallback will be removed.")
}
