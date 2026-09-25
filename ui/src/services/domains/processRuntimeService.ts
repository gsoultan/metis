import { processClient } from "../shared/connect";
import { raiseIfRefused } from "../raise";
import { requestJSON } from "../shared/rest";
import type { ApiAuditEntry, ApiSubProcess, ProcessVariables } from "../types";
import type { WaitingProcess } from "../../domain/processHeatmap";
import type { Deadlines } from "../../domain/slaReport";

type GetAuditLogsResponse = {
  entries?: ApiAuditEntry[];
  err?: string;
};

type ListSubProcessesResponse = {
  instances?: ApiSubProcess[];
  err?: string;
};

export const processRuntimeService = {
  /**
   * Starts an instance.
   *
   * `version` names one deliberately; omitted, the live version runs. Naming one
   * is how a staged version gets tried before it is promoted — the alternative
   * was to make it live for everybody and find out.
   */
  async startProcess(
    projectId: string,
    definitionKey: string,
    variables: ProcessVariables = {},
    version = 0,
    signal?: AbortSignal,
  ) {
    const response = await processClient.startProcess({ projectId, definitionKey, variables, version }, { signal });
    return { instance_id: raiseIfRefused(response).instanceId };
  },

  /**
   * One page of a project's process instances.
   *
   * A busy engine produces instances continuously, so this is the list most
   * likely to grow past what a browser can hold.
   */
  async listInstances(
    projectId: string,
    page?: { page: number; pageSize: number },
    filter?: { status?: string; definitionId?: string; needsAttention?: boolean },
    signal?: AbortSignal,
  ) {
    const response = await processClient.listInstances(
      {
        projectId,
        page: page ? { page: page.page, pageSize: page.pageSize } : undefined,
        // The server refuses a status it does not know, so an empty string has
        // to mean "every state" rather than being sent as a value.
        status: filter?.status ?? '',
        definitionId: filter?.definitionId ?? '',
        needsAttention: filter?.needsAttention ?? false,
      },
      { signal },
    );
    return {
      instances: response.instances ?? [],
      err: response.error,
      pageInfo: response.page
        ? {
            total: Number(response.page.total),
            page: response.page.page,
            pageSize: response.page.pageSize,
            hasMore: response.page.hasMore,
          }
        : undefined,
      // Counted across the project, not this page — what lets the list say
      // "12 need attention" while showing twenty-five completed runs.
      statusCounts: (response.statusCounts ?? []).map((count) => ({
        status: count.status,
        total: Number(count.total),
      })),
      // Which of these rows is waiting on a person, and how many are across the
      // project. Separate from status because the engine never marks an
      // instance failed — a job that runs out of retries raises an incident and
      // leaves the instance active.
      needsAttentionTotal: Number(response.needsAttentionTotal ?? 0),
      needsAttentionIds: response.needsAttentionIds ?? [],
    };
  },

  async getInstance(id: string, signal?: AbortSignal) {
    const response = await processClient.getInstance({ id }, { signal });
    return { instance: response.instance, err: response.error };
  },

  async getExecutionPath(id: string, signal?: AbortSignal) {
    const response = await processClient.getExecutionPath({ instanceId: id }, { signal });
    return { nodes: response.nodes ?? [], node_frequencies: response.nodeFrequencies ?? {}, err: response.error };
  },

  /** A project's open work with a due date, soonest first, read on the server. */
  async deadlines(projectId: string, signal?: AbortSignal): Promise<Deadlines> {
    const data = await requestJSON<{ deadlines?: Deadlines; err?: string }>(`/projects/${projectId}/deadlines`, {
      signal,
    });
    return raiseIfRefused(data).deadlines ?? {};
  },

  /** Where a project's running work is sitting now, counted on the server. */
  async waitingByStep(projectId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ processes?: WaitingProcess[]; err?: string }>(`/projects/${projectId}/waiting`, {
      signal,
    });
    return raiseIfRefused(data).processes ?? [];
  },

  async getAuditLogs(id: string, signal?: AbortSignal) {
    const data = await requestJSON<GetAuditLogsResponse>(`/instances/${id}/audit`, { signal });
    return { entries: data.entries ?? [], err: data.err };
  },

  async listSubProcesses(parentInstanceId: string, signal?: AbortSignal) {
    const data = await requestJSON<ListSubProcessesResponse>(`/instances/${parentInstanceId}/subprocesses`, { signal });
    return { instances: data.instances ?? [], err: data.err };
  },

  /**
   * Starts one step inside an ad-hoc sub-process.
   *
   * Deliberately repeatable: BPMN lets a step inside an ad-hoc sub-process run
   * any number of times, so asking twice starts it twice rather than being
   * quietly ignored. raiseIfRefused is what makes a refusal visible — this
   * endpoint reports one in the body, so without it a rejected activation
   * shows a green success toast and nothing happens.
   */
  async activateAdHocTask(
    instanceId: string,
    subProcessNodeId: string,
    taskNodeId: string,
    signal?: AbortSignal,
  ) {
    const data = await requestJSON<{ err?: string }>(`/processes/adhoc/activate`, {
      method: "POST",
      body: { instance_id: instanceId, sub_process_node_id: subProcessNodeId, task_node_id: taskNodeId },
      signal,
    });
    return raiseIfRefused(data);
  },

  /**
   * A project's history as an OCEL 2.0 object-centric event log.
   *
   * `includeVariables` is off unless asked for, and asking for it means asking
   * for every business fact the project has recorded — an amount, an
   * applicant's name, an approval decision. Mining a model needs the activity,
   * the case and the time, and none of those are in there.
   */
  async exportOCEL(projectId: string, includeVariables = false, signal?: AbortSignal) {
    const query = includeVariables ? "?include_variables=true" : "";
    const data = await requestJSON<{ log?: unknown; err?: string }>(
      `/projects/${projectId}/ocel${query}`,
      { signal },
    );
    return raiseIfRefused(data).log;
  },
};
