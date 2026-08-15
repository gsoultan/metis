package impl

import (
	"context"

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
}

func (h *UserTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// Full implementation: This delegates to the taskService which manages the lifecycle of human tasks.
	return h.taskService.CreateTaskForNode(ctx, *instance, node)
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
