package process

import (
	"context"
	"fmt"

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
	ActivateAdHocTask    endpoint.Endpoint
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
		ActivateAdHocTask:    MakeActivateAdHocTaskEndpoint(s),
		BroadcastSignal:      MakeBroadcastSignalEndpoint(s),
		SendMessage:          MakeSendMessageEndpoint(s),
		ExecuteScript:        MakeExecuteScriptEndpoint(s),
		ListSubProcesses:     MakeListSubProcessesEndpoint(s),
	}
}

func MakeStartProcessEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(StartProcessRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a StartProcessRequest, got %T", request)
		}
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
		req, ok := request.(ListInstancesRequest)
		if !ok {
			return ListInstancesResponse{Err: fmt.Errorf("unexpected request type %T", request)}, nil
		}

		// A project id that does not parse used to be discarded, leaving
		// uuid.Nil — which ListInstancesPaged reads as "no project filter" and
		// answers with every instance in the tenant. A malformed id must narrow
		// nothing.
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListInstancesResponse{
				Err: fmt.Errorf("project id %q is not a valid identifier: %w", req.ProjectID, err),
			}, nil
		}

		page, pageErr := s.ListInstancesPaged(ctx, projectID, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if pageErr != nil {
			return ListInstancesResponse{Err: pageErr}, nil
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
		req, ok := request.(GetExecutionPathRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a GetExecutionPathRequest, got %T", request)
		}
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
		req, ok := request.(GetAuditLogsRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a GetAuditLogsRequest, got %T", request)
		}
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
		req, ok := request.(GetInstanceRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a GetInstanceRequest, got %T", request)
		}
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
		req, ok := request.(ListSubProcessesRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a ListSubProcessesRequest, got %T", request)
		}
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
		req, ok := request.(GetProcessStatisticsRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a GetProcessStatisticsRequest, got %T", request)
		}
		projectID, err := optionalUUID(req.ProjectID)
		if err != nil {
			return GetProcessStatisticsResponse{Err: err}, nil
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

// MakeActivateAdHocTaskEndpoint starts one step inside an ad-hoc sub-process.
//
// It is scoped like the other operations on a running instance: the tenant and
// auth interceptors decide who may reach it. It does not consult the assignee or
// candidate list of the enclosing activity — those govern who may complete a
// task, and an ad-hoc sub-process has no task of its own.
func MakeActivateAdHocTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ActivateAdHocTaskRequest)
		if !ok {
			return ActivateAdHocTaskResponse{Err: fmt.Errorf("unexpected request type %T", request)}, nil
		}
		// The domain error travels in the response, as it does for every endpoint
		// here; the second return is for transport failures.
		return ActivateAdHocTaskResponse{Err: activateAdHocTask(ctx, s, req)}, nil
	}
}

func activateAdHocTask(ctx context.Context, s services.ServiceFacade, req ActivateAdHocTaskRequest) error {
	instanceID, err := uuid.Parse(req.InstanceID)
	if err != nil {
		return fmt.Errorf("instance id %q is not a valid identifier: %w", req.InstanceID, err)
	}
	return s.ActivateTask(ctx, instanceID, req.SubProcessNodeID, req.TaskNodeID)
}

func MakeBroadcastSignalEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(BroadcastSignalRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a BroadcastSignalRequest, got %T", request)
		}
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
		req, ok := request.(SendMessageRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a SendMessageRequest, got %T", request)
		}
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
		req, ok := request.(ExecuteScriptRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected a ExecuteScriptRequest, got %T", request)
		}
		vars, err := s.ExecuteScript(ctx, req.Script, req.ScriptFormat, req.Variables)
		return ExecuteScriptResponse{Variables: vars, Err: err}, nil
	}
}

// optionalUUID parses an id that a caller may legitimately omit.
//
// Empty means "not given" and yields uuid.Nil, which the repositories read as
// "do not filter on this". Anything else must be a real id: discarding the parse
// error mapped a typo onto uuid.Nil too, so a malformed organization id quietly
// widened the query to every organization the caller can see instead of being
// refused.
func optionalUUID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%q is not a valid id: %w", raw, err)
	}
	return id, nil
}
