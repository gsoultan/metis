package process

import (
	"context"
	"fmt"
	"sort"
	"strings"

	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories/models"
)

type Endpoints struct {
	StartProcess         endpoint.Endpoint
	GetInstance          endpoint.Endpoint
	ListInstances        endpoint.Endpoint
	GetExecutionPath     endpoint.Endpoint
	GetAuditLogs         endpoint.Endpoint
	ExportOCEL           endpoint.Endpoint
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
		ExportOCEL:           MakeExportOCELEndpoint(s),
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
			return StartProcessResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		// StartSubProcess with no parent is what StartProcess does; it is the
		// form that also carries a version. A zero version means the live one.
		id, err := s.StartSubProcess(ctx, projectID, req.DefinitionKey, req.Version, req.Variables, uuid.Nil, "")
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
				Err: apierr.Invalidf("project id %q is not a valid identifier: %v", req.ProjectID, err),
			}, nil
		}

		filter, filterErr := instanceFilterOf(req)
		if filterErr != nil {
			return ListInstancesResponse{Err: filterErr}, nil
		}

		page, pageErr := s.ListInstancesPaged(ctx, projectID, filter, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if pageErr != nil {
			return ListInstancesResponse{Err: pageErr}, nil
		}

		// Counted on every page rather than only the first. A caller that jumps
		// to page nine still needs to know what it is nine pages into, and the
		// grouped count runs off the same index the page itself walks.
		//
		// The chips describe the population the rows are drawn from, before any
		// choice the reader has made within it — this project, and this process
		// if one was named. Both narrowing choices are stripped here rather than
		// ignored further down: counting only the state already selected would
		// zero every other chip the moment somebody used one, and a layer that
		// discards part of what it is handed is a layer the next caller has to
		// already know about.
		countFilter := filter
		countFilter.Status = ""
		countFilter.NeedsAttention = false
		counts, countErr := s.CountInstancesByStatus(ctx, projectID, countFilter)
		if countErr != nil {
			return ListInstancesResponse{Err: countErr}, nil
		}

		// Which of these are waiting on a person, and how many are project-wide.
		// Bounded by the page for the marks and counted whole for the number.
		onPage := make([]uuid.UUID, len(page.Items))
		for i, instance := range page.Items {
			onPage[i] = instance.ID
		}
		attention, attentionErr := s.InstanceAttention(ctx, projectID, countFilter, onPage)
		if attentionErr != nil {
			return ListInstancesResponse{Err: attentionErr}, nil
		}

		return ListInstancesResponse{
			Instances: page.Items,
			Page: &InstancePageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
			StatusCounts:        statusCountsOf(counts),
			NeedsAttentionTotal: attention.Total,
			NeedsAttentionIDs:   needingAttention(page.Items, attention),
		}, nil
	}
}

// needingAttention lists the page's instances that hold an open incident.
//
// In the order they appear on the page rather than the order a map yields, so
// the response does not reshuffle between two identical requests.
func needingAttention(instances []entities.ProcessInstance, attention entities.InstanceAttention) []string {
	var ids []string
	for _, instance := range instances {
		if attention.NeedsAttention(instance.ID) {
			ids = append(ids, instance.ID.String())
		}
	}
	return ids
}

// instanceFilterOf validates what the caller asked to narrow by.
//
// An unrecognised status is refused rather than dropped. Dropping it answers a
// request for "everything that failed" with every instance in the project,
// which reads as "nothing failed" — the one answer this page must never give
// wrongly. A malformed definition id is refused for the same reason a malformed
// project id is: a filter that cannot be honoured must narrow nothing, and
// narrowing nothing means widening everything.
func instanceFilterOf(req ListInstancesRequest) (repocontracts.InstanceFilter, error) {
	var filter repocontracts.InstanceFilter

	if req.Status != "" {
		status := models.ProcessStatus(strings.ToLower(strings.TrimSpace(req.Status)))
		if !models.ValidProcessStatus(status) {
			return filter, apierr.Invalidf(
				"status %q is not a process state; expected one of %s",
				req.Status, strings.Join(processStatusNames(), ", "))
		}
		filter.Status = status
	}

	if req.DefinitionID != "" {
		definitionID, err := uuid.Parse(req.DefinitionID)
		if err != nil {
			return repocontracts.InstanceFilter{}, apierr.Invalidf(
				"definition id %q is not a valid identifier: %v", req.DefinitionID, err)
		}
		filter.DefinitionID = definitionID
	}

	filter.NeedsAttention = req.NeedsAttention

	return filter, nil
}

// processStatusNames lists the accepted states for an error message, so a
// caller that guessed wrong is told what to guess instead.
func processStatusNames() []string {
	names := make([]string, 0, len(models.ProcessStatuses))
	for _, status := range models.ProcessStatuses {
		names = append(names, string(status))
	}
	return names
}

// statusCountsOf renders the counts in a fixed order.
//
// Map iteration order is random in Go, so serving them straight out of the map
// would reorder a caller's filter chips on every refresh.
func statusCountsOf(counts map[models.ProcessStatus]int64) []InstanceStatusCount {
	out := make([]InstanceStatusCount, 0, len(counts))
	for _, status := range models.ProcessStatuses {
		if total, ok := counts[status]; ok {
			out = append(out, InstanceStatusCount{Status: string(status), Total: total})
		}
	}
	// A state the engine no longer writes can still be in the table from an
	// older version. Reporting it is better than a total that does not add up —
	// sorted, for the same reason the known ones are in a fixed order.
	var unknown []string
	for status := range counts {
		if !models.ValidProcessStatus(status) {
			unknown = append(unknown, string(status))
		}
	}
	sort.Strings(unknown)
	for _, status := range unknown {
		out = append(out, InstanceStatusCount{Status: status, Total: counts[models.ProcessStatus(status)]})
	}
	return out
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
			return GetAuditLogsResponse{Err: apierr.Invalidf("instance_id %q is not a valid identifier: %v", req.InstanceID, err)}, nil
		}
		entries, err := s.GetAuditLogs(ctx, id)
		return GetAuditLogsResponse{Entries: entries, Err: err}, nil
	}
}

func MakeExportOCELEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ExportOCELRequest)
		if !ok {
			return nil, fmt.Errorf("process: expected an ExportOCELRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ExportOCELResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		log, err := s.ExportOCEL(ctx, id, entities.OCELOptions{IncludeVariables: req.IncludeVariables})
		return ExportOCELResponse{Log: log, Err: err}, nil
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
			return GetInstanceResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
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
			return ListSubProcessesResponse{Err: apierr.Invalidf("parent_instance_id %q is not a valid identifier: %v", req.ParentInstanceID, err)}, nil
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
			CompletedTasks:     stats.CompletedTasks,
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
		return apierr.Invalidf("instance id %q is not a valid identifier: %v", req.InstanceID, err)
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
			return BroadcastSignalResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
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
			return SendMessageResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
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
