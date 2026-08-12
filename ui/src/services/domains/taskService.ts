import { taskClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type { ProcessVariables } from "../types";

type ListIncidentsResponse = {
  incidents?: unknown[];
  err?: string;
};

export const taskService = {
  async listTasks(projectId: string, signal?: AbortSignal) {
    const response = await taskClient.listTasks({ projectId }, { signal });
    return { tasks: response.tasks ?? [], err: response.error };
  },

  async completeTask(id: string, userId: string, variables: ProcessVariables = {}, signal?: AbortSignal) {
    const response = await taskClient.completeTask({ id, userId, variables }, { signal });
    return { err: response.error };
  },

  async claimTask(id: string, userId: string, signal?: AbortSignal) {
    const response = await taskClient.claimTask({ id, userId }, { signal });
    return { err: response.error };
  },

  async unclaimTask(id: string, signal?: AbortSignal) {
    const response = await taskClient.unclaimTask({ id }, { signal });
    return { err: response.error };
  },

  async delegateTask(id: string, userId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/delegate`, {
      method: "POST",
      body: { user_id: userId },
      signal,
    });
    return { err: data.err };
  },

  async updateTask(id: string, name: string, priority: number, dueDate?: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}`, {
      method: "PUT",
      body: { name, priority, due_date: dueDate },
      signal,
    });
    return { err: data.err };
  },

  async assignTask(id: string, userId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/assign`, {
      method: "POST",
      body: { user_id: userId },
      signal,
    });
    return { err: data.err };
  },

  async listTasksByCandidates(userId: string, groups: string[] = [], signal?: AbortSignal) {
    const response = await taskClient.listTasksByCandidates({ userId, groups }, { signal });
    return { tasks: response.tasks ?? [], err: response.error };
  },

  /**
   * One page of the tasks assigned to a user.
   *
   * `page` is optional on the wire, so omitting it asks the server for its
   * default window rather than for everything — the unbounded read is no
   * longer reachable from here.
   */
  async listTasksByAssignee(
    assignee: string,
    page?: { page: number; pageSize: number },
    signal?: AbortSignal,
  ) {
    const response = await taskClient.listTasksByAssignee(
      { assignee, page: page ? { page: page.page, pageSize: page.pageSize } : undefined },
      { signal },
    );
    return {
      tasks: response.tasks ?? [],
      err: response.error,
      // The server reports the window it actually served, after clamping, so
      // the controls reflect reality rather than what was asked for.
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

  async listIncidents(instanceId: string, signal?: AbortSignal) {
    const data = await requestJSON<ListIncidentsResponse>(`/incidents/${instanceId}`, { signal });
    return { incidents: data.incidents ?? [], err: data.err };
  },

  async resolveIncident(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/incidents/${id}/resolve`, {
      method: "POST",
      signal,
    });
    return { err: data.err };
  },
};
