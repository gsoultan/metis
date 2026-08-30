package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
	contracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// adHocActivator lets a knowledge worker run the steps inside an ad-hoc
// sub-process in whatever order the work turns out to need.
//
// Two design decisions are recorded here because the contract did not state
// them and nothing in the repository implemented it.
//
//  1. Who may activate. Activation is scoped exactly like the other operations
//     that act on a running instance — broadcasting a signal, sending a message
//     — which are authorised by the tenant and project interceptors on the way
//     in. It deliberately does not consult the assignee or candidate list of the
//     enclosing activity: those govern who may complete a task, and an ad-hoc
//     sub-process has no task of its own to hang that on. If activation should
//     be narrower than "may act on this instance", that is a policy to add at
//     the endpoint, where every other such rule lives.
//
//  2. When the sub-process finishes. The completion condition is re-evaluated
//     each time a step inside finishes. Any other reading makes the condition
//     unusable — it is written against the work done inside, so if it is never
//     re-checked it can never become true.
type adHocActivator struct {
	engine contracts.ExecutionEngine
}

// NewAdHocActivator creates the activator used to drive an ad-hoc sub-process.
func NewAdHocActivator(engine contracts.ExecutionEngine) contracts.AdHocActivator {
	return &adHocActivator{engine: engine}
}

// ActivateTask starts one step inside an ad-hoc sub-process.
//
// It is deliberately repeatable: BPMN allows a step inside an ad-hoc
// sub-process to be run any number of times, so asking twice starts it twice
// rather than being quietly ignored.
func (a *adHocActivator) ActivateTask(ctx context.Context, instanceID uuid.UUID, subProcessNodeID, taskNodeID string) error {
	instance, err := a.engine.GetInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("activate %q: load instance %s: %w", taskNodeID, instanceID, err)
	}
	if instance.Status != entities.ProcessActive {
		return fmt.Errorf("activate %q: instance %s is %s, not running", taskNodeID, instanceID, instance.Status)
	}
	if instance.Definition == nil {
		return fmt.Errorf("activate %q: instance %s has no definition reference", taskNodeID, instanceID)
	}

	def, err := a.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return fmt.Errorf("activate %q: load definition: %w", taskNodeID, err)
	}

	subProcess := def.FindNode(subProcessNodeID)
	if subProcess == nil {
		return fmt.Errorf("activate %q: there is no step called %q in this process", taskNodeID, subProcessNodeID)
	}
	if !subProcess.IsAdHoc {
		return fmt.Errorf("activate %q: %q is not an ad-hoc sub-process, so its steps run in the order they are drawn",
			taskNodeID, subProcessNodeID)
	}

	// The sub-process has to be the one the process is currently in. Without
	// this, a step could be started in a sub-process the process has not reached
	// or has already left.
	if len(instance.GetTokensByNode(subProcess)) == 0 {
		return fmt.Errorf("activate %q: the process is not currently inside %q", taskNodeID, subProcessNodeID)
	}

	target := findAdHocChild(def, subProcess, taskNodeID)
	if target == nil {
		return fmt.Errorf("activate %q: it is not one of the steps inside %q", taskNodeID, subProcessNodeID)
	}

	instance.AddToken(target)
	if err := a.engine.UpdateInstance(ctx, instance); err != nil {
		return fmt.Errorf("activate %q: %w", taskNodeID, err)
	}
	if err := a.engine.ExecuteNode(ctx, &instance, def, target.ID); err != nil {
		return fmt.Errorf("activate %q: %w", taskNodeID, err)
	}
	return nil
}

// IsComplete reports whether the ad-hoc sub-process has satisfied its
// completion condition.
//
// No condition means there is nothing to wait for, which is the same reading
// the handler uses when the sub-process is first entered.
func (a *adHocActivator) IsComplete(_ context.Context, instance *entities.ProcessInstance, subProcessNode *entities.Node) (bool, error) {
	if instance == nil || subProcessNode == nil {
		return false, fmt.Errorf("ad-hoc completion check needs both an instance and a sub-process")
	}
	if subProcessNode.CompletionCondition == "" {
		return true, nil
	}
	return logic.GetConditionEvaluatorChain().Evaluate(subProcessNode.CompletionCondition, instance.Variables), nil
}

// findAdHocChild locates a step inside the sub-process, accepting both the
// nested shape and the flat one where children carry a parent id.
func findAdHocChild(def *entities.ProcessDefinition, subProcess *entities.Node, taskNodeID string) *entities.Node {
	for _, child := range subProcess.Nodes {
		if child.ID == taskNodeID {
			return child
		}
	}
	for _, node := range def.Nodes {
		if node.ID == taskNodeID && node.ParentID == subProcess.ID {
			return node
		}
	}
	return nil
}
