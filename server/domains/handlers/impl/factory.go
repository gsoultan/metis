package impl

import (
	"github.com/gsoultan/metis/server/domains/entities"
	handlercontracts "github.com/gsoultan/metis/server/domains/handlers/contracts"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

type nodeHandlerFactory struct {
	engine              servicecontracts.ExecutionEngine
	taskService         servicecontracts.TaskService
	jobService          servicecontracts.JobService
	externalTaskService servicecontracts.ExternalTaskService
	decisionService     servicecontracts.DecisionService
	connectorService    servicecontracts.ConnectorService
	subRepo             contracts.SubscriptionRepository
	auditWriter         servicecontracts.AuditWriter
}

// NewNodeHandlerFactory creates a new NodeHandlerFactory implementation.
//
// It takes no ExpressionEvaluator. That dependency was a FEEL evaluator, and
// FEEL here is a DMN decision-table cell matcher — it resolves an expression
// against variables["_input"], not against process variables. Handing it to
// process-node handlers invited exactly one bug: the ad-hoc sub-process
// completion condition was evaluated with it and could never be true. Process
// conditions go through logic.GetConditionEvaluatorChain(), the same chain
// gateways use; DMN cells keep their evaluator inside the decision service.
func NewNodeHandlerFactory(
	engine servicecontracts.ExecutionEngine,
	taskService servicecontracts.TaskService,
	jobService servicecontracts.JobService,
	externalTaskService servicecontracts.ExternalTaskService,
	decisionService servicecontracts.DecisionService,
	connectorService servicecontracts.ConnectorService,
	subRepo contracts.SubscriptionRepository,
	auditWriter servicecontracts.AuditWriter,
) handlercontracts.NodeHandlerFactory {
	return &nodeHandlerFactory{
		engine:              engine,
		taskService:         taskService,
		jobService:          jobService,
		externalTaskService: externalTaskService,
		decisionService:     decisionService,
		connectorService:    connectorService,
		subRepo:             subRepo,
		auditWriter:         auditWriter,
	}
}

func (f *nodeHandlerFactory) GetHandler(nodeType entities.NodeType) (handlercontracts.NodeHandler, error) {
	var internal handlercontracts.InternalNodeHandler

	switch nodeType {
	case entities.StartEvent:
		internal = &StartEventHandler{f.engine}
	case entities.ServiceTask:
		internal = &ServiceTaskHandler{f.jobService, f.externalTaskService}
	case entities.UserTask:
		internal = &UserTaskHandler{f.taskService, f.decisionService, f.auditWriter}
	case entities.EndEvent:
		internal = &EndEventHandler{f.engine}
	case entities.TerminateEndEvent:
		internal = &TerminateEndEventHandler{f.engine}
	case entities.ExclusiveGateway:
		internal = &ExclusiveGatewayHandler{f.engine}
	case entities.ParallelGateway:
		internal = &ParallelGatewayHandler{f.engine}
	case entities.InclusiveGateway:
		internal = &InclusiveGatewayHandler{f.engine}
	case entities.EventBasedGateway:
		internal = &EventBasedGatewayHandler{f.engine}
	case entities.IntermediateCatchEvent, entities.TimerEvent:
		internal = &IntermediateCatchEventHandler{f.jobService, f.subRepo}
	case entities.IntermediateThrowEvent:
		internal = &IntermediateThrowEventHandler{f.engine}
	case entities.SignalEvent:
		internal = &SignalThrowEventHandler{f.engine}
	case entities.MessageEvent:
		internal = &MessageThrowEventHandler{f.engine}
	case entities.ScriptTask:
		internal = &ScriptTaskHandler{f.engine}
	case entities.CallActivity:
		internal = &CallActivityHandler{f.engine}
	case entities.SubProcess:
		adHoc := NewAdHocSubProcessHandler(f.engine)
		internal = &SubProcessHandler{engine: f.engine, adHocHandler: adHoc}
	case entities.BoundaryEvent:
		internal = &BoundaryEventHandler{f.engine}
	case entities.ErrorEndEvent:
		internal = &ErrorEndEventHandler{}
	case entities.EscalationThrowEvent:
		internal = &EscalationThrowEventHandler{f.engine}
	case entities.CompensationThrowEvent:
		internal = &CompensationThrowEventHandler{f.engine}
	case entities.ManualTask:
		internal = &ManualTaskHandler{f.taskService}
	case entities.BusinessRuleTask:
		internal = &BusinessRuleTaskHandler{f.engine, f.decisionService, f.auditWriter}
	default:
		return &NullNodeHandler{}, nil
	}

	return NewNodeHandlerTemplate(f.engine, internal), nil
}
