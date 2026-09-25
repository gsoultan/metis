import { useQuery } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import { useInvalidateOnEvents } from './useEventStream';

// The queryFn ternary returned the service's real result on one branch and a
// hand-written literal on the other. TypeScript widened that union to `{}`,
// so every property access on the result failed once the processService
// facade stopped being typed `any`. Deriving the fallback from the service's
// own signature keeps both branches the same shape.
type StatisticsResult = Awaited<ReturnType<typeof processService.getProcessStatistics>>;
type InstanceResult = Awaited<ReturnType<typeof processService.getInstance>>;

export const useProcessStatistics = () => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    queryKey: ['stats', currentProjectId],
    queryFn: ({ signal }) =>
      (currentProjectId && token)
        ? processService.getProcessStatistics(currentProjectId, signal)
        : Promise.resolve({ stats: undefined, err: '' } as unknown as StatisticsResult),
    enabled: !!currentProjectId && !!token,
  });
};

export const useInstance = (id: string | null) => {
  return useQuery({
    queryKey: ['instance', id],
    queryFn: ({ signal }) =>
      id
        ? processService.getInstance(id, signal)
        : Promise.resolve({ instance: undefined, err: '' } as InstanceResult),
    enabled: !!id,
  });
};

export const useExecutionPath = (id: string | null) => {
  return useQuery({
    queryKey: ['execution-path', id],
    queryFn: ({ signal }) =>
      id ? processService.getExecutionPath(id, signal) : Promise.resolve({ nodes: [], node_frequencies: {}, err: "" }),
    enabled: !!id,
  });
};

export const useAuditLogs = (id: string | null) => {
  return useQuery({
    queryKey: ['audit-logs', id],
    queryFn: ({ signal }) =>
      id ? processService.getAuditLogs(id, signal) : Promise.resolve({ entries: [], err: "" }),
    enabled: !!id,
  });
};

export const useSubProcesses = (parentInstanceId: string | null) => {
  return useQuery({
    queryKey: ['subProcesses', parentInstanceId],
    queryFn: ({ signal }) =>
      parentInstanceId ? processService.listSubProcesses(parentInstanceId, signal) : Promise.resolve({ instances: [], err: "" }),
    enabled: !!parentInstanceId,
  });
};

type InstancesResult = Awaited<ReturnType<typeof processService.listInstances>>;

/** What the caller asked the server to narrow the list to. */
export interface InstanceFilter {
  /** One lifecycle state, or undefined for every state. */
  status?: string;
  /** One process definition, or undefined for every process. */
  definitionId?: string;
  /** Only instances holding an unresolved incident. */
  needsAttention?: boolean;
}

/**
 * One page of a project's instances, narrowed in the database.
 *
 * The filter is part of the query key rather than applied to the result. That
 * is the whole difference between this and what it replaced: narrowing the
 * twenty-five rows that came back cannot find the twelve failures in a project
 * of half a million, and a control that appears to search the project while
 * searching only the screen answers "show me everything that is broken" with
 * "nothing is".
 *
 * `refetchInterval` because these rows are live. An instance list that only
 * changes when somebody presses a button is a screenshot of the engine, and
 * the operator watching it for a failure has no reason to think it is stale.
 */
export const useInstances = (page = 1, pageSize = 25, filter: InstanceFilter = {}, live = true) => {
  const { currentProjectId, token } = useAppStore();
  const { status, definitionId, needsAttention } = filter;
  return useQuery({
    // The page and the filter are part of the key, so stepping back to a page
    // already seen is a cache hit rather than a refetch.
    queryKey: [
      'instances', currentProjectId, page, pageSize,
      status ?? '', definitionId ?? '', needsAttention ?? false,
    ],
    queryFn: ({ signal }) =>
      currentProjectId && token
        ? processService.listInstances(
            currentProjectId, { page, pageSize }, { status, definitionId, needsAttention }, signal)
        : Promise.resolve({
            instances: [], err: '', pageInfo: undefined, statusCounts: [],
            needsAttentionTotal: 0, needsAttentionIds: [],
          } as InstancesResult),
    enabled: !!currentProjectId && !!token,
    // Hold the current rows while the next page loads, so the table does not
    // collapse into a skeleton on every click — and, with polling on, so it
    // does not flicker every few seconds either.
    placeholderData: (previous) => previous,
    refetchInterval: live ? LIVE_REFRESH_MS : false,
    // Deliberately not while the tab is in the background: nobody is reading
    // it, and a browser left open on this page overnight would otherwise spend
    // the night polling.
    refetchIntervalInBackground: false,
  });
};

/**
 * How often the list re-reads itself.
 *
 * Five seconds is short enough that somebody watching a process move through
 * its steps sees it move, and long enough that a room of operators with this
 * page open is not a load generator: each poll is one indexed page plus one
 * grouped count.
 */
export const LIVE_REFRESH_MS = 5000;
/** The events after which work may have moved to another step. */
const WAITING_EVENTS = ['ProcessStarted', 'ProcessCompleted', 'NodeReached', 'TaskCreated', 'TaskCompleted', 'TaskCanceled'] as const;

/**
 * Where the current project's running work is sitting, for the heat map.
 *
 * Refetched when work moves rather than polled: the old heat map polled a page
 * of instances every five seconds and still missed everything past the page.
 */
export const useWaitingByStep = () => {
  const { currentProjectId } = useAppStore();
  useInvalidateOnEvents(WAITING_EVENTS, ['waiting-by-step', currentProjectId]);
  return useQuery({
    queryKey: ['waiting-by-step', currentProjectId],
    queryFn: ({ signal }) => processService.waitingByStep(currentProjectId as string, signal),
    enabled: !!currentProjectId,
  });
};
