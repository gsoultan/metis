import { taskClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type { DelegatedTask, ProcessVariables } from "../types";
import { raiseIfRefused } from "../raise";
import { queueRequest } from "../../pwa/outbox";
import { outboxAvailable } from "../../pwa/outboxStore";

type ListIncidentsResponse = {
  incidents?: unknown[];
  err?: string;
};

type ListDelegatedTasksResponse = {
  tasks?: DelegatedTask[];
  page?: { total: number; page: number; page_size: number; has_more: boolean };
  err?: string;
};

type PageRequest = { page: number; pageSize: number };

type WirePageInfo = { total: bigint | number | string; page: number; pageSize: number; hasMore: boolean };

/**
 * The window the server actually served, after clamping — so paging controls
 * reflect reality rather than what was asked for.
 */
function toPageInfo(page: WirePageInfo | undefined) {
  if (!page) {
    return undefined;
  }
  return {
    total: Number(page.total),
    page: page.page,
    pageSize: page.pageSize,
    hasMore: page.hasMore,
  };
}

function toWirePage(page: PageRequest | undefined) {
  return page ? { page: page.page, pageSize: page.pageSize } : undefined;
}

/**
 * The acting user is whoever the token says. The server ignores any user id in
 * a claim, complete, delegate or assign body, so none is sent — a request that
 * named somebody else was never a way to act as them, only a way to lie to the
 * audit trail.
 *
 * Every read raises on refusal. A list that came back as `{ tasks: [], err }`
 * rendered as "You're all caught up", which is the one thing a failed read
 * must never look like.
 */
export const taskService = {
  /** One page of a project's tasks. */
  async listTasks(projectId: string, page?: PageRequest, signal?: AbortSignal) {
    const response = raiseIfRefused(
      await taskClient.listTasks({ projectId, page: toWirePage(page) }, { signal }),
    );
    return { tasks: response.tasks ?? [], pageInfo: toPageInfo(response.page) };
  },

  /*
   * Completing and claiming are the two things somebody does from a phone,
   * often with no signal — so these two, and only these two, fall back to the
   * outbox. A write with wider consequences (deleting an organization, say)
   * still fails outright rather than being applied minutes later against state
   * nobody re-checked.
   *
   * The queued form is the REST route rather than this Connect call, because
   * that is what the outbox replays with an Idempotency-Key the server honours.
   */
  async completeTask(id: string, variables: ProcessVariables = {}, signal?: AbortSignal, taskName?: string) {
    // Asked before trying, not inferred from the failure afterwards.
    //
    // The transports here do not share one code path — the Connect client does
    // not go through window.fetch — so "was that a lost connection or a
    // refusal?" is answered differently depending on which one ran. Deciding up
    // front makes the offline case deterministic; the catch below still covers
    // losing the connection mid-request.
    if (isOffline()) {
      return queueTaskAction(id, "complete", { variables }, taskName);
    }
    try {
      const response = await taskClient.completeTask({ id, variables }, { signal });
      return { err: raiseIfRefused(response).error, queued: false };
    } catch (error) {
      if (!shouldQueue(error)) throw error;
      return queueTaskAction(id, "complete", { variables }, taskName);
    }
  },

  async claimTask(id: string, signal?: AbortSignal, taskName?: string) {
    if (isOffline()) {
      return queueTaskAction(id, "claim", {}, taskName);
    }
    try {
      const response = await taskClient.claimTask({ id }, { signal });
      return { err: raiseIfRefused(response).error, queued: false };
    } catch (error) {
      if (!shouldQueue(error)) throw error;
      return queueTaskAction(id, "claim", {}, taskName);
    }
  },

  async unclaimTask(id: string, signal?: AbortSignal) {
    const response = await taskClient.unclaimTask({ id }, { signal });
    return { err: raiseIfRefused(response).error };
  },

  /**
   * Releases a task somebody else holds, saying why.
   *
   * The holder's own release (unclaimTask) goes over Connect, whose message
   * has no reason in it; the server takes a release from anybody else only
   * with one, and this route carries it.
   */
  async releaseTaskFor(id: string, reason: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/unclaim`, {
      method: "POST",
      body: { reason },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  /*
   * A reason is carried when one is given. The task's holder gives none; the
   * server refuses anybody else who does not say why.
   */
  async delegateTask(id: string, userId: string, reason?: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/delegate`, {
      method: "POST",
      body: reason ? { user_id: userId, reason } : { user_id: userId },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async updateTask(id: string, name: string, priority: number, dueDate?: string, reason?: string, signal?: AbortSignal) {
    const fields = { name, priority, due_date: dueDate };
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}`, {
      method: "PUT",
      body: reason ? { ...fields, reason } : fields,
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  async assignTask(id: string, userId: string, reason?: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/assign`, {
      method: "POST",
      body: reason ? { user_id: userId, reason } : { user_id: userId },
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  /**
   * Hands a delegated task back to whoever delegated it. The delegate gives no
   * reason; an administrator doing it for them must.
   */
  async resolveTask(id: string, reason?: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/tasks/${id}/resolve`, {
      method: "POST",
      body: reason ? { reason } : {},
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },

  /**
   * One page of the tasks the signed-in person delegated that are still with
   * their delegate. Who is asking comes from the token.
   */
  async listTasksDelegatedByMe(page?: PageRequest, signal?: AbortSignal) {
    const query = page ? `?page=${page.page}&page_size=${page.pageSize}` : "";
    const data = raiseIfRefused(
      await requestJSON<ListDelegatedTasksResponse>(`/tasks/delegated${query}`, { signal }),
    );
    const tasks = data.tasks ?? [];
    return { tasks, total: data.page?.total ?? tasks.length };
  },

  /**
   * One page of the unclaimed tasks the signed-in user could take. The server
   * works out the candidate groups from the caller's memberships; the client
   * sends nothing about who is asking.
   */
  async listTasksByCandidates(page?: PageRequest, signal?: AbortSignal) {
    const response = raiseIfRefused(
      await taskClient.listTasksByCandidates({ page: toWirePage(page) }, { signal }),
    );
    return { tasks: response.tasks ?? [], pageInfo: toPageInfo(response.page) };
  },

  /**
   * One page of the tasks assigned to a user.
   *
   * `page` is optional on the wire, so omitting it asks the server for its
   * default window rather than for everything — the unbounded read is no
   * longer reachable from here.
   */
  async listTasksByAssignee(assignee: string, page?: PageRequest, signal?: AbortSignal) {
    const response = raiseIfRefused(
      await taskClient.listTasksByAssignee({ assignee, page: toWirePage(page) }, { signal }),
    );
    return { tasks: response.tasks ?? [], pageInfo: toPageInfo(response.page) };
  },

  async listIncidents(instanceId: string, signal?: AbortSignal) {
    const data = raiseIfRefused(await requestJSON<ListIncidentsResponse>(`/incidents/${instanceId}`, { signal }));
    return { incidents: data.incidents ?? [] };
  },

  async resolveIncident(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/incidents/${id}/resolve`, {
      method: "POST",
      signal,
    });
    return { err: raiseIfRefused(data).err };
  },
};

/** Whether there is no connection to try, and somewhere to keep the work. */
function isOffline(): boolean {
  if (!outboxAvailable()) return false;
  return typeof navigator !== "undefined" && navigator.onLine === false;
}

/**
 * Whether a failure is the connection rather than the server's answer.
 *
 * A refusal must not be queued: the server understood and said no, and sending
 * it again in ten minutes would only get the same no. Only a request that never
 * reached anybody is worth keeping.
 */
function shouldQueue(error: unknown): boolean {
  if (!outboxAvailable()) return false;
  if (typeof navigator !== "undefined" && navigator.onLine === false) return true;
  // A fetch that never got an answer rejects with a TypeError; anything the
  // server said comes back as an ordinary Error carrying its message.
  return error instanceof TypeError;
}

/** Records one task action for sending later, and describes it in words. */
async function queueTaskAction(
  id: string,
  action: "complete" | "claim",
  body: Record<string, unknown>,
  taskName?: string,
) {
  const verb = action === "complete" ? "Complete" : "Claim";
  await queueRequest({
    method: "POST",
    path: `/tasks/${id}/${action}`,
    body,
    label: taskName ? `${verb} "${taskName}"` : `${verb} a task`,
  });
  return { err: undefined, queued: true };
}
