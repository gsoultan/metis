package process

import (
	"context"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/services"
)

type Endpoints struct {
	StartProcess         endpoint.Endpoint
	GetInstance          endpoint.Endpoint
	ListInstances        endpoint.Endpoint
	GetExecutionPath     endpoint.Endpoint
	GetAuditLogs         endpoint.Endpoint
	GetProcessStatistics endpoint.Endpoint
	BroadcastSignal      endpoint.Endpoint
	SendMessage          endpoint.Endpoint
	ExecuteScript        endpoint.Endpoint
	ListSubProcesses     endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		StartProcess:         MakeStartProcessEndpoint(s),
		GetInstance:          MakeGetInstanceEndpoint(s),
		ListInstances:        MakeListInstancesEndpoint(s),
		GetExecutionPath:     MakeGetExecutionPathEndpoint(s),
		GetAuditLogs:         MakeGetAuditLogsEndpoint(s),
		GetProcessStatistics: MakeGetProcessStatisticsEndpoint(s),
		BroadcastSignal:      MakeBroadcastSignalEndpoint(s),
		SendMessage:          MakeSendMessageEndpoint(s),
		ExecuteScript:        MakeExecuteScriptEndpoint(s),
		ListSubProcesses:     MakeListSubProcessesEndpoint(s),
	}
}

func MakeStartProcessEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(StartProcessRequest)
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return StartProcessResponse{Err: err}, nil
		}
		id, err := s.StartProcess(ctx, projectID, req.DefinitionKey, req.Variables)
		return StartProcessResponse{InstanceID: id, Err: err}, nil
	}
}

func MakeListInstancesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(ListInstancesRequest)
		projectID, _ := uuid.Parse(req.ProjectID)

		page, err := s.ListInstancesPaged(ctx, projectID, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if err != nil {
			return ListInstancesResponse{Err: err}, nil
		}
		return ListInstancesResponse{
			Instances: page.Items,
			Page: &InstancePageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

func MakeGetExecutionPathEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(GetExecutionPathRequest)
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return GetExecutionPathResponse{Error: err.Error()}, nil
		}
		path, err := s.GetExecutionPath(ctx, id)
		if err != nil {
			return GetExecutionPathResponse{Error: err.Error()}, nil
		}
		return GetExecutionPathResponse{
			Nodes:       path.Nodes,
			Frequencies: path.Frequencies,
		}, nil
	}
}

func MakeGetAuditLogsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(GetAuditLogsRequest)
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return GetAuditLogsResponse{Err: err}, nil
		}
		entries, err := s.GetAuditLogs(ctx, id)
		return GetAuditLogsResponse{Entries: entries, Err: err}, nil
	}
}

func MakeGetInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(GetInstanceRequest)
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetInstanceResponse{Err: err}, nil
		}
		inst, err := s.GetInstance(ctx, id)
		return GetInstanceResponse{Instance: inst, Err: err}, nil
	}
}

func MakeListSubProcessesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(ListSubProcessesRequest)
		id, err := uuid.Parse(req.ParentInstanceID)
		if err != nil {
			return ListSubProcessesResponse{Err: err}, nil
		}
		instances, err := s.ListSubProcesses(ctx, id)
		return ListSubProcessesResponse{Instances: instances, Err: err}, nil
	}
}

func MakeGetProcessStatisticsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(GetProcessStatisticsRequest)
		var projectID uuid.UUID
		if req.ProjectID != "" {
			projectID, _ = uuid.Parse(req.ProjectID)
		}
		stats, err := s.GetProcessStatistics(ctx, projectID)
		if err != nil {
			return GetProcessStatisticsResponse{Err: err}, nil
		}
		return GetProcessStatisticsResponse{
			ActiveInstances:    stats.ActiveInstances,
			CompletedInstances: stats.CompletedInstances,
			FailedInstances:    stats.FailedInstances,
			TotalTasks:         stats.TotalTasks,
			PendingTasks:       stats.PendingTasks,
			NodeFrequencies:    stats.NodeFrequencies,
		}, nil
	}
}

func MakeBroadcastSignalEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(BroadcastSignalRequest)
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return BroadcastSignalResponse{Err: err}, nil
		}
		err = s.BroadcastSignal(ctx, projectID, req.SignalName, req.Variables)
		return BroadcastSignalResponse{Err: err}, nil
	}
}

func MakeSendMessageEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(SendMessageRequest)
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SendMessageResponse{Err: err}, nil
		}
		err = s.SendMessage(ctx, projectID, req.MessageName, req.CorrelationKey, req.Variables)
		return SendMessageResponse{Err: err}, nil
	}
}

func MakeExecuteScriptEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(ExecuteScriptRequest)
		vars, err := s.ExecuteScript(ctx, req.Script, req.ScriptFormat, req.Variables)
		return ExecuteScriptResponse{Variables: vars, Err: err}, nil
	}
}
