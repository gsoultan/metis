package impl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlercontracts "github.com/gsoultan/gobpm/server/domains/handlers/contracts"
	"github.com/gsoultan/gobpm/server/domains/logic"
	observerContracts "github.com/gsoultan/gobpm/server/domains/observers/contracts"
	serviceContracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// Engine is the concrete BPMN execution engine.  It is exported so that the
// composition root (service.go) can call the wiring helpers (SetJobService etc.)
// without exposing those methods on the ExecutionEngine interface, satisfying ISP.
// All other consumers should depend on the serviceContracts.ExecutionEngine interface.
type Engine struct {
	repo           repositories.Repository
	handlerFactory handlercontracts.NodeHandlerFactory
	dispatcher     observerContracts.EventDispatcher
	jobSvc         serviceContracts.JobService
	varHistory     serviceContracts.VariableHistoryWriter
}

// EngineOption is a functional option for configuring an Engine after construction.
// Options are used by the composition root (service.go) to resolve circular
// dependencies (engine ↔ jobSvc, engine ↔ handlerFactory) without exposing
// mutable public setter methods on the Engine type.
type EngineOption func(*Engine)

// WithJobService injects the JobService used for timer and service-task enqueueing.
func WithJobService(js serviceContracts.JobService) EngineOption {
	return func(e *Engine) { e.jobSvc = js }
}

// WithHandlerFactory injects the factory used to resolve BPMN node handlers.
func WithHandlerFactory(hf handlercontracts.NodeHandlerFactory) EngineOption {
	return func(e *Engine) { e.handlerFactory = hf }
}

// WithVariableHistoryService injects the snapshot writer for variable history.
func WithVariableHistoryService(vh serviceContracts.VariableHistoryWriter) EngineOption {
	return func(e *Engine) { e.varHistory = vh }
}

// NewExecutionEngine constructs an Engine with its mandatory dependencies.
// Pass EngineOption values (WithJobService, WithHandlerFactory, etc.) to inject
// collaborators that depend on the engine itself (circular dependencies).
// Returns the concrete *Engine so the composition root can apply options; assign
// the result to serviceContracts.ExecutionEngine at the public API boundary.
func NewExecutionEngine(
	repo repositories.Repository,
	dispatcher observerContracts.EventDispatcher,
	opts ...EngineOption,
) *Engine {
	e := &Engine{repo: repo, dispatcher: dispatcher}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Apply runs additional EngineOptions on an already-constructed Engine.
// This is used by the composition root to inject collaborators that couldn't
// be provided at construction time due to circular dependencies.
func (e *Engine) Apply(opts ...EngineOption) {
	for _, opt := range opts {
		opt(e)
	}
}

func (e *Engine) StartProcess(ctx context.Context, projectID uuid.UUID, definitionKey string, vars map[string]any) (uuid.UUID, error) {
	return e.StartSubProcess(ctx, projectID, definitionKey, 0, vars, uuid.Nil, "")
}

func (e *Engine) StartSubProcess(ctx context.Context, projectID uuid.UUID, definitionKey string, version int, vars map[string]any, parentInstanceID uuid.UUID, parentNodeID string) (uuid.UUID, error) {
	var instanceID uuid.UUID
	err := e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		cmd := NewStartProcessCommand(e, projectID, definitionKey, version, vars, parentInstanceID, parentNodeID)
		err := cmd.Execute(txCtx)
		instanceID = cmd.InstanceID
		return err
	})
	return instanceID, err
}

func (e *Engine) startProcessInternal(ctx context.Context, projectID uuid.UUID, definitionKey string, version int, vars map[string]any, parentInstanceID uuid.UUID, parentNodeID string) (uuid.UUID, error) {
	var m models.ProcessDefinitionModel
	var err error
	if version > 0 {
		m, err = e.repo.Definition().GetByKeyAndVersion(ctx, definitionKey, version)
	} else {
		m, err = e.repo.Definition().GetByKey(ctx, definitionKey)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not find definition: %w", err)
	}
	def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()

	startNode := def.GetStartNode()
	if startNode == nil {
		return uuid.Nil, fmt.Errorf("definition %s has no start event", definitionKey)
	}

	idObj, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate instance id: %w", err)
	}

	instance := entities.ProcessInstance{
		ID:         idObj,
		Project:    &entities.Project{ID: projectID},
		Definition: &entities.ProcessDefinition{ID: def.ID},
		Status:     entities.ProcessActive,
		Variables:  vars,
		CreatedAt:  time.Now(),
	}
	instance.AddToken(startNode)

	if parentInstanceID != uuid.Nil {
		instance.ParentInstance = &entities.ProcessInstance{ID: parentInstanceID}
		instance.ParentNode = &entities.Node{ID: parentNodeID}
	}

	err = e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		_, err := e.repo.Process().Create(txCtx, adapters.InstanceModelAdapter{Instance: instance}.ToModel())
		if err != nil {
			return err
		}

		// Activate Event Sub-processes start events for the process level
		for _, node := range def.Nodes {
			if !node.IsEventSubProcess || node.ParentID != "" {
				continue
			}
			// Find start event in this event sub-process
			for _, sn := range def.Nodes {
				if sn.ParentID != node.ID || sn.Type != entities.StartEvent {
					continue
				}
				if err := e.activateEventNode(txCtx, &instance, sn); err != nil {
					return fmt.Errorf("activate event sub-process start %s: %w", sn.ID, err)
				}
			}
		}

		e.dispatcher.Dispatch(txCtx, entities.ProcessEvent{
			Type:      entities.EventProcessStarted,
			Instance:  &instance,
			Project:   instance.Project,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
		})

		return e.ExecuteNode(txCtx, &instance, def, startNode.ID)
	})

	return idObj, err
}

func (e *Engine) GetInstance(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error) {
	m, err := e.repo.Process().Get(ctx, id)
	if err != nil {
		return entities.ProcessInstance{}, err
	}
	return adapters.InstanceEntityAdapter{Model: m}.ToEntity(), nil
}

func (e *Engine) GetInstanceForUpdate(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error) {
	m, err := e.repo.Process().GetForUpdate(ctx, id)
	if err != nil {
		return entities.ProcessInstance{}, err
	}
	return adapters.InstanceEntityAdapter{Model: m}.ToEntity(), nil
}

func (e *Engine) GetProcessDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error) {
	m, err := e.repo.Definition().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return adapters.DefinitionEntityAdapter{Model: m}.ToEntity(), nil
}

func (e *Engine) ListInstances(ctx context.Context, projectID uuid.UUID) ([]entities.ProcessInstance, error) {
	var ms []models.ProcessInstanceModel
	var err error
	if projectID != uuid.Nil {
		ms, err = e.repo.Process().ListByProject(ctx, projectID)
	} else {
		ms, err = e.repo.Process().List(ctx)
	}
	if err != nil {
		return nil, err
	}
	res := make([]entities.ProcessInstance, len(ms))
	for i, m := range ms {
		res[i] = adapters.InstanceEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (e *Engine) ListSubProcesses(ctx context.Context, parentInstanceID uuid.UUID) ([]entities.ProcessInstance, error) {
	ms, err := e.repo.Process().ListByParent(ctx, parentInstanceID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.ProcessInstance, len(ms))
	for i, m := range ms {
		res[i] = adapters.InstanceEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

// GetRootInstance walks the parent chain starting from instanceID and returns
// the top-level ancestor. Stops if a cycle is detected (max 100 hops).
func (e *Engine) GetRootInstance(ctx context.Context, instanceID uuid.UUID) (entities.ProcessInstance, error) {
	const maxDepth = 100
	current, err := e.GetInstance(ctx, instanceID)
	if err != nil {
		return entities.ProcessInstance{}, fmt.Errorf("GetRootInstance: load instance: %w", err)
	}
	for depth := range maxDepth {
		if current.ParentInstance == nil {
			return current, nil
		}
		parent, err := e.GetInstance(ctx, current.ParentInstance.ID)
		if err != nil {
			return entities.ProcessInstance{}, fmt.Errorf("GetRootInstance: load parent at depth %d: %w", depth, err)
		}
		current = parent
	}
	return current, nil
}

func (e *Engine) GetExecutionPath(ctx context.Context, instanceID uuid.UUID) (entities.ExecutionPath, error) {
	entries, err := e.repo.Audit().ListByInstance(ctx, instanceID)
	if err != nil {
		return entities.ExecutionPath{}, err
	}

	var nodes []*entities.Node
	frequencies := make(map[string]int)
	seen := make(map[string]bool)

	// Audit logs are usually ordered by timestamp desc. We want chronological order.
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Type == entities.EventNodeReached && entry.NodeID != "" {
			frequencies[entry.NodeID]++
			if !seen[entry.NodeID] {
				nodes = append(nodes, &entities.Node{ID: entry.NodeID})
				seen[entry.NodeID] = true
			}
		}
	}
	return entities.ExecutionPath{
		Nodes:       nodes,
		Frequencies: frequencies,
	}, nil
}

func (e *Engine) GetAuditLogs(ctx context.Context, instanceID uuid.UUID) ([]entities.AuditEntry, error) {
	ms, err := e.repo.Audit().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.AuditEntry, len(ms))
	for i, m := range ms {
		res[i] = adapters.AuditEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

// maxExecutionDepth bounds how many nodes a single synchronous execution may
// traverse before the engine refuses to continue.
//
// ExecuteNode → handler → Proceed → followOutgoingFlows → ExecuteNode is
// mutually recursive with no natural base case: a definition that loops back to
// an earlier task — a retry loop, an entirely ordinary modelling pattern —
// recurses once per iteration inside a single transaction, and enough
// iterations overflow the stack and take down the whole server, since this runs
// on the HTTP handler goroutine.
//
// Raising an incident at a bound is not a fix for the recursion; it converts an
// unrecoverable crash into a diagnosable BPMN error naming the node that
// looped. The real fix is an async continuation model, which is tracked in the
// execution plan.
const maxExecutionDepth = 200

// envMaxExecutionDepth overrides maxExecutionDepth for deployments with
// legitimately deep synchronous processes.
const envMaxExecutionDepth = "GOBPM_MAX_EXECUTION_DEPTH"

type executionDepthKey struct{}

// executionDepthLimit returns the configured traversal bound.
func executionDepthLimit() int {
	if raw := os.Getenv(envMaxExecutionDepth); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return maxExecutionDepth
}

// enterNode increments the traversal depth for this execution, returning an
// error once the bound is exceeded.
func enterNode(ctx context.Context, nodeID string) (context.Context, error) {
	// An absent depth means this is the first node of the execution.
	depth := 0
	if current, ok := ctx.Value(executionDepthKey{}).(int); ok {
		depth = current
	}
	depth++
	if limit := executionDepthLimit(); depth > limit {
		return nil, fmt.Errorf(
			"BPMN_ERROR:execution exceeded %d nodes at %q; the definition most likely contains an "+
				"unbounded loop. Break the cycle, or raise %s if the process is legitimately this deep",
			limit, nodeID, envMaxExecutionDepth)
	}
	return context.WithValue(ctx, executionDepthKey{}, depth), nil
}

func (e *Engine) ExecuteNode(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error {
	return e.ExecuteNodeIteration(ctx, instance, def, nodeID, "")
}

func (e *Engine) ExecuteNodeIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error {
	ctx, err := enterNode(ctx, nodeID)
	if err != nil {
		return err
	}
	return e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		cmd := NewExecuteNodeCommand(e, instance, def, nodeID, iterationID)
		return cmd.Execute(txCtx)
	})
}

func (e *Engine) executeNodeInternal(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error {
	node := def.FindNode(nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found", nodeID)
	}

	e.dispatcher.Dispatch(ctx, entities.ProcessEvent{
		Type:      entities.EventNodeReached,
		Instance:  instance,
		Project:   instance.Project,
		Node:      node,
		Timestamp: time.Now().Unix(),
		Variables: instance.Variables,
	})

	handler, err := e.handlerFactory.GetHandler(node.Type)
	if err != nil {
		return err
	}

	// Boundary Events: activation.
	//
	// A failure here must abort the node. If a subscription or timer is not
	// persisted, the boundary event can never fire and the instance waits
	// forever with no incident raised — the failure would be invisible.
	events := def.GetBoundaryEvents(node.ID)
	for _, event := range events {
		if err := e.activateEventNode(ctx, instance, event); err != nil {
			return fmt.Errorf("activate boundary event %s on node %s: %w", event.ID, node.ID, err)
		}
	}

	err = handler.Execute(ctx, instance, def, *node, iterationID)
	if err != nil {
		bpmnError := ""
		if strings.HasPrefix(err.Error(), "BPMN_ERROR:") {
			bpmnError = strings.TrimPrefix(err.Error(), "BPMN_ERROR:")
		}

		// Boundary Events: check for error boundary events
		events := def.GetBoundaryEvents(node.ID)
		for _, event := range events {
			catchCode := event.GetStringProperty("error_code")
			if catchCode != "" {
				if bpmnError != "" && (catchCode == bpmnError || catchCode == "*") {
					return e.Proceed(ctx, instance, def, event.ID)
				}
				if bpmnError == "" && catchCode == "*" {
					return e.Proceed(ctx, instance, def, event.ID)
				}
			}
		}
		return err
	}

	return nil
}

func (e *Engine) Proceed(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error {
	return e.ProceedIteration(ctx, instance, def, nodeID, "")
}

func (e *Engine) ProceedIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error {
	return e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		cmd := NewProceedCommand(e, instance, def, nodeID, iterationID)
		return cmd.Execute(txCtx)
	})
}

// proceedInternal advances a process instance past nodeID at the end of the
// current transaction.  It is called by ProceedIteration via UnitOfWork.Do.
func (e *Engine) proceedInternal(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error {
	node := def.FindNode(nodeID)

	// Step 1: handle interrupting boundary events.
	if err := e.handleBoundaryInterrupt(ctx, instance, def, node); err != nil {
		return err
	}

	// Step 2: remove the token (simple case) or check multi-instance completion.
	done, err := e.removeOrCheckMultiInstance(ctx, instance, def, node, nodeID, iterationID)
	if err != nil || !done {
		return err // not done yet — wait for remaining iterations
	}

	// A step inside an ad-hoc sub-process finishing is what makes its completion
	// condition worth re-reading: the condition is written against the work done
	// inside, so it can only become true here.
	if done, err := e.checkAdHocCompletion(ctx, instance, def, node); err != nil || done {
		return err
	}

	// Step 3: clean up sibling tokens and subscriptions.
	if err := e.cleanupEventBasedGatewaySiblings(ctx, instance, def, nodeID); err != nil {
		return err
	}
	if err := e.cleanupSubscriptions(ctx, instance, nodeID, def); err != nil {
		return err
	}

	// Step 4: mark node completed and follow outgoing flows.
	instance.MarkCompleted(node)
	return e.followOutgoingFlows(ctx, instance, def, nodeID)
}

// handleBoundaryInterrupt removes the host activity token when an interrupting
// boundary event fires and cleans up related subscriptions.
//
// Deletion errors are returned, not ignored: a subscription that outlives its
// node can re-trigger an already-completed activity when a later signal or
// message arrives, producing duplicate execution.

// cancelOpenTasksForNode withdraws any task still open for nodeID.
//
// Removing an activity's token cancels it as far as the engine is concerned,
// but the user task it created is a separate row and nothing was closing it.
// The work stayed in whoever's inbox it was assigned to, and completing it acted
// on an activity the process had already abandoned.
//
// A failure here is returned: an interrupt that half-happened — token gone, task
// still offered — is worse than one that reports itself.
func (e *Engine) cancelOpenTasksForNode(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if node == nil {
		return nil
	}
	ms, err := e.repo.Task().ListByInstance(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("list tasks for instance %s: %w", instance.ID, err)
	}

	for i := range ms {
		m := &ms[i]
		if m.NodeID != node.ID {
			continue
		}
		if m.Status != models.TaskUnclaimed && m.Status != models.TaskClaimed && m.Status != models.TaskDelegated {
			continue
		}
		if err := e.repo.Task().UpdateStatus(ctx, uuid.UUID(m.ID), models.TaskCanceled); err != nil {
			return fmt.Errorf("cancel task %s on node %s: %w", m.ID, node.ID, err)
		}
		e.dispatcher.Dispatch(ctx, entities.ProcessEvent{
			Type:      entities.EventTaskCanceled,
			Instance:  instance,
			Project:   instance.Project,
			Node:      node,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
		})
	}
	return nil
}

func (e *Engine) handleBoundaryInterrupt(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node) error {
	if node == nil || node.Type != entities.BoundaryEvent || node.AttachedToRef == "" {
		return nil
	}
	// A non-interrupting boundary event notifies and leaves the work running:
	// its activity keeps its token and its other boundary events stay armed.
	// That is what lets a repeating timer nag while an approval is still open.
	if node.IsNonInterrupting() {
		return nil
	}

	hostNode := def.FindNode(node.AttachedToRef)
	if hostNode != nil {
		instance.RemoveTokenByNode(hostNode)
		if err := e.cancelOpenTasksForNode(ctx, instance, hostNode); err != nil {
			return err
		}
	}
	for _, ev := range def.GetBoundaryEvents(node.AttachedToRef) {
		if err := e.repo.Subscription().DeleteByNode(ctx, instance.ID, ev.ID); err != nil {
			return fmt.Errorf("delete subscription for boundary event %s: %w", ev.ID, err)
		}
	}
	return nil
}

// checkAdHocCompletion re-evaluates the completion condition of the ad-hoc
// sub-process a finished step belongs to, and lets the process through when it
// is satisfied.
//
// Returns true when it advanced the process, so the caller stops treating the
// finished step as an ordinary node.
func (e *Engine) checkAdHocCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node) (bool, error) {
	if node == nil || node.ParentID == "" {
		return false, nil
	}
	parent := def.FindNode(node.ParentID)
	if parent == nil || !parent.IsAdHoc {
		return false, nil
	}
	if len(instance.GetTokensByNode(parent)) == 0 {
		return false, nil
	}
	if parent.CompletionCondition != "" &&
		!logic.GetConditionEvaluatorChain().Evaluate(parent.CompletionCondition, instance.Variables) {
		// More work to do inside; the sub-process keeps waiting.
		return false, e.UpdateInstance(ctx, *instance)
	}

	return true, e.Proceed(ctx, instance, def, parent.ID)
}

// removeOrCheckMultiInstance handles token removal for both simple and multi-instance
// nodes.  Returns (true, nil) when execution should continue past the node.
func (e *Engine) removeOrCheckMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	if node == nil || node.MultiInstanceType == "" || node.MultiInstanceType == "none" {
		instance.RemoveTokenByNode(node)
		return true, nil
	}
	return e.checkMultiInstanceCompletion(ctx, instance, def, node, nodeID, iterationID)
}

// checkMultiInstanceCompletion increments the completion counter and returns
// (true, nil) when all iterations are done (or the completion condition is met).
func (e *Engine) checkMultiInstanceCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	completed, total := instance.CompleteMultiInstanceIteration(nodeID)
	instance.RemoveTokenByIteration(node, iterationID)

	conditionMet := completed >= total
	if node.CompletionCondition != "" {
		// The condition sees the business variables plus BPMN's own progress
		// counters, without either being written back to the instance.
		conditionMet = logic.GetConditionEvaluatorChain().
			Evaluate(node.CompletionCondition, instance.MultiInstanceConditionScope(nodeID))
	}

	if !conditionMet {
		if err := e.UpdateInstance(ctx, *instance); err != nil {
			return false, err
		}
		// Sequential means one at a time: the next iteration is started when
		// this one finishes, and nothing was starting it. A task set to run
		// once per supplier ran for the first supplier and the process then sat
		// there, with no error and no task, looking like it was still working.
		if node.MultiInstanceType == "sequential" && completed < total {
			return false, e.startNextSequentialIteration(ctx, instance, def, node, completed)
		}
		return false, nil
	}

	// Every iteration is done, so the bookkeeping goes.
	instance.FinishMultiInstance(nodeID)
	return true, nil
}

// startNextSequentialIteration runs iteration `index` of a sequential
// multi-instance node.
//
// Each iteration is started from within the one before it, so a collection of
// n runs n deep. The engine's execution-depth bound applies, which is the same
// protection an accidental loop gets: a very long collection is reported as
// exceeding it rather than exhausting the stack.
func (e *Engine) startNextSequentialIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, index int) error {
	iterationID := fmt.Sprintf("%d", index)
	instance.AddTokenWithIteration(node, iterationID)

	collection, _ := entities.MultiInstanceCollection(instance, *node)
	entities.BindMultiInstanceElement(instance, *node, collection, index)

	if err := e.UpdateInstance(ctx, *instance); err != nil {
		return err
	}
	return e.ExecuteNodeIteration(ctx, instance, def, node.ID, iterationID)
}

// cleanupEventBasedGatewaySiblings cancels competing tokens when one branch of
// an event-based gateway is taken.
func (e *Engine) cleanupEventBasedGatewaySiblings(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error {
	for _, inFlow := range def.GetIncomingFlows(nodeID) {
		src := def.FindNode(inFlow.SourceRef)
		if src == nil || src.Type != entities.EventBasedGateway {
			continue
		}
		for _, siblingFlow := range def.GetOutgoingFlows(src.ID) {
			if siblingFlow.TargetRef == nodeID {
				continue
			}
			if targetNode := def.FindNode(siblingFlow.TargetRef); targetNode != nil {
				instance.RemoveTokenByNode(targetNode)
			}
			if err := e.repo.Subscription().DeleteByNode(ctx, instance.ID, siblingFlow.TargetRef); err != nil {
				return fmt.Errorf("cancel event-gateway sibling %s: %w", siblingFlow.TargetRef, err)
			}
		}
	}
	return nil
}

// cleanupSubscriptions removes subscriptions for boundary events attached to
// nodeID and the catch-event subscription for nodeID itself.
func (e *Engine) cleanupSubscriptions(ctx context.Context, instance *entities.ProcessInstance, nodeID string, def *entities.ProcessDefinition) error {
	for _, ev := range def.GetBoundaryEvents(nodeID) {
		if err := e.repo.Subscription().DeleteByNode(ctx, instance.ID, ev.ID); err != nil {
			return fmt.Errorf("delete subscription for boundary event %s: %w", ev.ID, err)
		}
	}
	if err := e.repo.Subscription().DeleteByNode(ctx, instance.ID, nodeID); err != nil {
		return fmt.Errorf("delete subscription for node %s: %w", nodeID, err)
	}
	return nil
}

// followOutgoingFlows adds tokens to every target node and executes them.
func (e *Engine) followOutgoingFlows(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error {
	flows := def.GetOutgoingFlows(nodeID)
	if len(flows) == 0 {
		return e.UpdateInstance(ctx, *instance)
	}
	for _, flow := range flows {
		targetNode := def.FindNode(flow.TargetRef)
		if targetNode != nil {
			instance.AddToken(targetNode)
		}
		if err := e.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		if err := e.ExecuteNode(ctx, instance, def, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

// extractInt reads an integer process variable that may be stored as float64 (JSON default) or int.
func extractInt(vars map[string]any, key string) int {
	val, ok := vars[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// activateEventNode registers the signal/message subscriptions and timer job
// that let a catching event node fire later.
//
// Every failure is returned rather than logged-and-ignored: an event node whose
// subscription was not persisted is a token that can never advance, and the
// process would hang with no incident to investigate.
func (e *Engine) activateEventNode(ctx context.Context, instance *entities.ProcessInstance, node *entities.Node) error {
	if signalName := node.GetStringProperty("signal_name"); signalName != "" {
		sub := entities.NewSignalSubscription(instance.Project, instance, node, signalName)
		if err := e.repo.Subscription().Create(ctx, adapters.SubscriptionModelAdapter{Subscription: sub}.ToModel()); err != nil {
			return fmt.Errorf("create signal subscription %q for node %s: %w", signalName, node.ID, err)
		}
	}
	if messageName := node.GetStringProperty("message_name"); messageName != "" {
		correlationKey, err := entities.ResolveCorrelationKey(node.GetStringProperty("correlation_key"), instance.Variables)
		if err != nil {
			return fmt.Errorf("resolve correlation key for message %q on node %s: %w", messageName, node.ID, err)
		}
		sub := entities.NewMessageSubscription(instance.Project, instance, node, messageName, correlationKey)
		if err := e.repo.Subscription().Create(ctx, adapters.SubscriptionModelAdapter{Subscription: sub}.ToModel()); err != nil {
			return fmt.Errorf("create message subscription %q for node %s: %w", messageName, node.ID, err)
		}
	}
	if duration := node.GetStringProperty("timer_duration"); duration != "" && e.jobSvc != nil {
		if err := e.jobSvc.EnqueueTimer(ctx, *instance, *node, duration); err != nil {
			return fmt.Errorf("enqueue timer %q for node %s: %w", duration, node.ID, err)
		}
	}
	return nil
}

func (e *Engine) UpdateInstance(ctx context.Context, instance entities.ProcessInstance) error {
	if err := e.repo.Process().Update(ctx, adapters.InstanceModelAdapter{Instance: instance}.ToModel()); err != nil {
		return err
	}
	e.captureVariableSnapshot(ctx, instance)
	return nil
}

// captureVariableSnapshot writes a variable snapshot when a
// VariableHistoryWriter is configured.
//
// Snapshots are observability, not execution state, so a failure is logged
// rather than propagated — losing a history row must not fail a business
// transaction. It is logged, though: the previous version discarded the error
// while its comment claimed otherwise, so failures were invisible.
func (e *Engine) captureVariableSnapshot(ctx context.Context, instance entities.ProcessInstance) {
	if e.varHistory == nil {
		return
	}
	snap := entities.VariableSnapshot{
		Instance:   &instance,
		Variables:  maps.Clone(instance.Variables),
		CapturedAt: time.Now(),
	}
	if err := e.varHistory.CaptureSnapshot(ctx, snap); err != nil {
		log.Warn().Err(err).
			Str("instanceId", instance.ID.String()).
			Msg("failed to capture variable snapshot")
	}
}

func (e *Engine) DispatchEvent(ctx context.Context, event entities.ProcessEvent) {
	e.dispatcher.Dispatch(ctx, event)
}

func (e *Engine) BroadcastSignal(ctx context.Context, projectID uuid.UUID, signalName string, vars map[string]any) error {
	ms, err := e.repo.Subscription().FindSignals(ctx, projectID, signalName)
	if err != nil {
		return err
	}

	// A signal is a broadcast: every waiting instance is entitled to it, so one
	// subscriber that fails must not silence the rest. Failures are collected and
	// reported together — the same choice TriggerCompensation makes, and for the
	// same reason. Returning on the first one delivered the signal to whichever
	// subscribers happened to sort earlier and skipped the others without a word.
	var errs []error
	for _, m := range ms {
		sub := adapters.SubscriptionEntityAdapter{Model: m}.ToEntity()
		if err := e.triggerSubscription(ctx, sub, vars); err != nil {
			errs = append(errs, fmt.Errorf("trigger signal subscription %s: %w", sub.ID, err))
		}
	}

	// Signal start events are a separate audience; a failed subscriber must not
	// stop the signal from starting the processes that wait for it.
	if err := e.triggerStartEvents(ctx, projectID, "signal_name", signalName, vars); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (e *Engine) SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error {
	ms, err := e.repo.Subscription().FindMessages(ctx, projectID, messageName, correlationKey)
	if err != nil {
		return err
	}

	// A correlated message normally has a single recipient, but an uncorrelated
	// one fans out to every instance waiting on that message name, so the same
	// all-or-report rule applies.
	var errs []error
	for _, m := range ms {
		sub := adapters.SubscriptionEntityAdapter{Model: m}.ToEntity()
		if err := e.triggerSubscription(ctx, sub, vars); err != nil {
			errs = append(errs, fmt.Errorf("trigger message subscription %s: %w", sub.ID, err))
		}
	}

	// Message start events carry no correlation key, so they are only in scope
	// for an uncorrelated message.
	if correlationKey == "" {
		if err := e.triggerStartEvents(ctx, projectID, "message_name", messageName, vars); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// triggerSubscription advances the instance waiting on sub, merging vars into
// its variables.
//
// The whole read-modify-write runs inside one UnitOfWork. GetForUpdate takes a
// SELECT ... FOR UPDATE row lock, and outside a transaction that lock is
// released the instant the statement returns — so the previous version, which
// loaded the instance before opening the transaction, provided no protection at
// all. Two messages correlating to the same instance could each read the same
// state and the second write would silently discard the first one's variables.
func (e *Engine) triggerSubscription(ctx context.Context, sub entities.EventSubscription, vars map[string]any) error {
	if sub.Instance == nil || sub.Node == nil {
		return fmt.Errorf("subscription %s has no instance or node reference", sub.ID)
	}

	return e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := e.repo.Process().GetForUpdate(txCtx, sub.Instance.ID)
		if err != nil {
			return fmt.Errorf("lock instance %s: %w", sub.Instance.ID, err)
		}
		instance := adapters.InstanceEntityAdapter{Model: m}.ToEntity()
		if instance.Definition == nil {
			return fmt.Errorf("instance %s has no definition reference", instance.ID)
		}

		md, err := e.repo.Definition().Get(txCtx, instance.Definition.ID)
		if err != nil {
			return fmt.Errorf("load definition %s: %w", instance.Definition.ID, err)
		}
		def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

		for k, v := range vars {
			instance.SetVariable(k, v)
		}

		if err := e.repo.Subscription().Delete(txCtx, sub.ID); err != nil {
			return fmt.Errorf("delete subscription %s: %w", sub.ID, err)
		}
		return e.Proceed(txCtx, &instance, def, sub.Node.ID)
	})
}

// triggerStartEvents starts an instance of every definition in the project whose
// start event declares propName == propValue (a signal or message start event).
//
// A failure to start any one definition is returned. Previously both the list
// error and every StartProcess error were discarded, so a signal that should
// have started five processes could start none and report success.
func (e *Engine) triggerStartEvents(ctx context.Context, projectID uuid.UUID, propName, propValue string, vars map[string]any) error {
	ms, err := e.repo.Definition().ListByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list definitions for project %s: %w", projectID, err)
	}

	for _, m := range ms {
		def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
		for _, node := range def.Nodes {
			if node.Type != entities.StartEvent || node.GetStringProperty(propName) != propValue {
				continue
			}
			if _, err := e.StartProcess(ctx, projectID, def.Key, vars); err != nil {
				return fmt.Errorf("start process %s from %s %q: %w", def.Key, propName, propValue, err)
			}
		}
	}
	return nil
}

// TriggerEscalation walks from the throwing node up through its ancestors,
// offering the escalation to each level's boundary events and event
// sub-processes until one takes it.
//
// "Did a handler take it" and "did the handler fail" are separate answers. They
// used to share one error return, so a handler that failed for a real reason —
// a database error, a failure inside the handler path — read as "nothing here"
// and the escalation was passed up to an outer handler as well. That both hid
// the failure and ran a second handler for one escalation.
func (e *Engine) TriggerEscalation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, escalationCode string) error {
	currNodeID := node.ID
	if node.ParentID != "" {
		currNodeID = node.ParentID
	}

	for currNodeID != "" {
		handled, err := e.checkBoundaryEscalation(ctx, instance, def, currNodeID, escalationCode)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}

		handled, err = e.checkEventSubProcessEscalation(ctx, instance, def, currNodeID, escalationCode)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}

		parent := def.FindNode(currNodeID)
		if parent != nil {
			currNodeID = parent.ParentID
		} else {
			currNodeID = ""
		}
	}

	// BPMN 2.0 treats an escalation as a notification, not a fault: one that
	// reaches the top uncaught is tolerated rather than raised. It is logged so
	// a model that reports something nobody listens for is still visible.
	log.Warn().
		Str("instanceId", instance.ID.String()).
		Str("nodeId", node.ID).
		Str("escalationCode", escalationCode).
		Msg("Escalation reached the top of the process with no handler")
	return nil
}

// isEscalationBoundary reports whether be is an escalation catch event.
//
// GetBoundaryEvents returns boundary events of every kind attached to a node,
// and error, timer, message and compensation events all report an empty
// escalation_code. Without this check the "no code catches anything" rule below
// matched all of them, so an escalation was delivered to whichever boundary
// event happened to come first — routinely an error handler written for a
// completely different situation.
func isEscalationBoundary(be *entities.Node) bool {
	return be.GetStringProperty("event_type") == "escalation" ||
		be.GetStringProperty("escalation_code") != ""
}

// checkBoundaryEscalation offers the escalation to nodeID's boundary events.
// It reports whether one took it, separately from whether that handler failed.
func (e *Engine) checkBoundaryEscalation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID, escalationCode string) (bool, error) {
	for _, be := range def.GetBoundaryEvents(nodeID) {
		if !isEscalationBoundary(be) {
			continue
		}
		// BPMN 2.0: an escalation boundary event with no escalationRef catches
		// any escalation. That catch-all belongs to escalation events only.
		if code := be.GetStringProperty("escalation_code"); code != escalationCode && code != "" {
			continue
		}
		if err := e.Proceed(ctx, instance, def, be.ID); err != nil {
			return true, fmt.Errorf("escalation boundary event %s on node %s: %w", be.ID, nodeID, err)
		}
		return true, nil
	}
	return false, nil
}

// checkEventSubProcessEscalation offers the escalation to event sub-processes at
// nodeID's level, matching on the start event's escalation code.
func (e *Engine) checkEventSubProcessEscalation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID, escalationCode string) (bool, error) {
	siblings := def.Nodes
	if parent := def.FindNode(nodeID); parent != nil {
		siblings = parent.Nodes
	}

	for _, sib := range siblings {
		if !sib.IsEventSubProcess {
			continue
		}
		for _, sn := range sib.Nodes {
			if sn.Type != entities.StartEvent || sn.GetStringProperty("escalation_code") != escalationCode {
				continue
			}
			instance.AddToken(sn)
			if err := e.ExecuteNode(ctx, instance, def, sn.ID); err != nil {
				return true, fmt.Errorf("escalation event sub-process %s start %s: %w", sib.ID, sn.ID, err)
			}
			return true, nil
		}
	}
	return false, nil
}

// TriggerCompensation compensates either a single referenced activity or, when
// activityRef is empty, every completed activity in reverse order.
//
// A compensation that fails is a business rollback that did not happen, so the
// error is surfaced rather than discarded. Remaining activities are still
// attempted first — abandoning the loop on the first failure would leave the
// instance even less consistent — and the failures are then reported together.
func (e *Engine) TriggerCompensation(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, activityRef string) error {
	if activityRef != "" {
		if refNode := def.FindNode(activityRef); refNode != nil {
			if err := e.compensateActivity(ctx, instance, def, refNode); err != nil {
				return fmt.Errorf("compensate activity %s: %w", activityRef, err)
			}
		}
		return e.Proceed(ctx, instance, def, node.ID)
	}

	// Compensate all in reverse order.
	var errs []error
	for i := len(instance.CompletedNodes) - 1; i >= 0; i-- {
		n := instance.CompletedNodes[i]
		if err := e.compensateActivity(ctx, instance, def, n); err != nil {
			errs = append(errs, fmt.Errorf("compensate activity %s: %w", n.ID, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return e.Proceed(ctx, instance, def, node.ID)
}

func (e *Engine) compensateActivity(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node) error {
	if node == nil {
		return nil
	}
	if instance.IsCompensated(node) {
		return nil
	}
	boundaryEvents := def.GetBoundaryEvents(node.ID)
	for _, be := range boundaryEvents {
		// A boundary event is a compensation event if it has a specific property or type.
		if be.GetStringProperty("event_type") == "compensation" || be.GetStringProperty("compensation") == "true" {
			instance.MarkCompensated(node)
			return e.Proceed(ctx, instance, def, be.ID)
		}
	}
	return nil
}

func (e *Engine) ExecuteScript(ctx context.Context, script string, scriptFormat string, variables map[string]any) (map[string]any, error) {
	if scriptFormat != "javascript" && scriptFormat != "" {
		return nil, fmt.Errorf("unsupported script format: %s", scriptFormat)
	}

	return logic.RunScript(ctx, script, variables)
}

// ListInstancesPaged returns one page of process instances with the total.
//
// projectID of uuid.Nil lists across the active tenant; the repository applies
// tenant scoping either way, so an unscoped read is not reachable from here.
func (e *Engine) ListInstancesPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.ProcessInstance], error) {
	var result repocontracts.Page[models.ProcessInstanceModel]
	var err error
	if projectID != uuid.Nil {
		result, err = e.repo.Process().ListByProjectPaged(ctx, projectID, page)
	} else {
		result, err = e.repo.Process().ListPaged(ctx, page)
	}
	if err != nil {
		return repocontracts.Page[entities.ProcessInstance]{}, err
	}

	instances := make([]entities.ProcessInstance, len(result.Items))
	for i, m := range result.Items {
		instances[i] = adapters.InstanceEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(instances, result.Total, page), nil
}
