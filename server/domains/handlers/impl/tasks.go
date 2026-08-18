package impl

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// ServiceTaskHandler handles automated tasks that call external services.
//
// Execution strategy:
//   - If the node has an ExternalTopic, it creates an external task record for a
//     worker to claim (pull model).
//   - Otherwise the task is queued as a job.  The job worker resolves connectors
//     and HTTP calls, updates instance variables and calls engine.Proceed when
//     done.  This keeps the handler free of HTTP/connector logic and prevents the
//     double-execution bug that occurred when connector code ran here AND in the
//     job worker.
type ServiceTaskHandler struct {
	jobService          contracts.JobService
	externalTaskService contracts.ExternalTaskService
}

func (h *ServiceTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// 1. External Task: create a pull-model task and return – the worker completes it.
	if node.ExternalTopic != "" {
		return h.externalTaskService.Create(ctx, &entities.ExternalTask{
			Project:           instance.Project,
			ProcessInstance:   instance,
			ProcessDefinition: &entities.ProcessDefinition{ID: instance.Definition.ID},
			Node:              &node,
			Topic:             node.ExternalTopic,
			Variables:         instance.Variables,
			Retries:           3,
		})
	}

	// 2. Enqueue an asynchronous job.  The job worker resolves any configured
	//    connector or HTTP endpoint, stores results, and calls engine.Proceed.
	return h.jobService.EnqueueServiceTask(ctx, *instance, node, iterationID)
}

// UserTaskHandler handles tasks that require human intervention.
type UserTaskHandler struct {
	taskService contracts.TaskService

	// decisionService resolves who should do the work, when the node says a
	// decision table decides that rather than the diagram. See assignment.go.
	decisionService contracts.DecisionService
	auditWriter     contracts.AuditWriter
}

func (h *UserTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	node = h.resolveAssignment(ctx, instance, node)
	// The taskService manages the lifecycle of human tasks.
	return h.taskService.CreateTaskForNode(ctx, *instance, node)
}

// resolveAssignment asks the node's assignment table who should do the work.
//
// A node that names no table is returned untouched, which is every node that
// existed before this. A table that fails is logged and the diagram's own
// assignment stands: a process that stops because an approval matrix could not
// be read is worse than one that routes to the default approver and says so.
func (h *UserTaskHandler) resolveAssignment(ctx context.Context, instance *entities.ProcessInstance, node entities.Node) entities.Node {
	key, version := assignmentDecisionOf(node)
	if key == "" || h.decisionService == nil {
		return node
	}

	result, err := h.decisionService.Evaluate(ctx, key, version, instance.Variables)
	if err != nil {
		log.Error().Err(err).
			Str("instance", instance.ID.String()).
			Str("node", node.ID).
			Str("decision", key).
			Msg("Could not decide who should do this task; the diagram's own assignment stands")
		return node
	}

	resolved := applyAssignment(node, result.Values)
	h.recordAssignment(ctx, instance, node, key, result)
	return resolved
}

// recordAssignment puts the choice on the timeline.
//
// "Why did this land on the CFO's desk?" is asked about approvals more than
// about anything else, and the answer is a table and a line in it.
func (h *UserTaskHandler) recordAssignment(
	ctx context.Context,
	instance *entities.ProcessInstance,
	node entities.Node,
	key string,
	result entities.DecisionResult,
) {
	if h.auditWriter == nil {
		return
	}
	nodeCopy := node
	entry := entities.AuditEntry{
		Instance:  &entities.ProcessInstance{ID: instance.ID},
		Node:      &nodeCopy,
		Type:      EventDecisionEvaluated,
		Message:   describeAssignment(key, result.Values),
		Timestamp: time.Now(),
		Data: map[string]any{
			"decision_key":     result.DecisionKey,
			"decision_name":    result.DecisionName,
			"decision_version": result.DecisionVersion,
			"matched_rules":    result.MatchedRules,
			"matched_rule_ids": result.MatchedRuleIDs,
			"outputs":          result.Values,
			"decided":          "assignment",
		},
	}
	if instance.Project != nil {
		entry.Project = &entities.Project{ID: instance.Project.ID}
	}
	if err := h.auditWriter.RecordEvent(ctx, entry); err != nil {
		log.Error().Err(err).Msg("Could not record how a task was assigned; the assignment stands")
	}
}

// ManualTaskHandler handles manual tasks that require human intervention but usually represent physical actions.
type ManualTaskHandler struct {
	taskService contracts.TaskService
}

func (h *ManualTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// Like UserTask, ManualTask creates a task entry that must be completed.
	return h.taskService.CreateTaskForNode(ctx, *instance, node)
}

// PassThroughHandler handles tasks that don't have a specific implementation yet, acting as a passthrough.
type PassThroughHandler struct {
	engine contracts.EngineRunner
}

func (h *PassThroughHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}
