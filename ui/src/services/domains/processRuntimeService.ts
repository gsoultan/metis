import { processClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type { ApiAuditEntry, ApiSubProcess, ProcessVariables } from "../types";

type GetAuditLogsResponse = {
  entries?: ApiAuditEntry[];
  err?: string;
};

type ListSubProcessesResponse = {
  instances?: ApiSubProcess[];
  err?: string;
};

export const processRuntimeService = {
  async startProcess(projectId: string, definitionKey: string, variables: ProcessVariables = {}, signal?: AbortSignal) {
    const response = await processClient.startProcess({ projectId, definitionKey, variables }, { signal });
    return { instance_id: response.instanceId, err: response.error };
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
    signal?: AbortSignal,
  ) {
    const response = await processClient.listInstances(
      { projectId, page: page ? { page: page.page, pageSize: page.pageSize } : undefined },
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

  async getAuditLogs(id: string, signal?: AbortSignal) {
    const data = await requestJSON<GetAuditLogsResponse>(`/instances/${id}/audit`, { signal });
    return { entries: data.entries ?? [], err: data.err };
  },

  async listSubProcesses(parentInstanceId: string, signal?: AbortSignal) {
    const data = await requestJSON<ListSubProcessesResponse>(`/instances/${parentInstanceId}/subprocesses`, { signal });
    return { instances: data.instances ?? [], err: data.err };
  },
};
