import { useQuery } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

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

export const useInstances = (page = 1, pageSize = 25) => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    // The page is part of the key, so stepping back to a page already seen is
    // a cache hit rather than a refetch.
    queryKey: ['instances', currentProjectId, page, pageSize],
    queryFn: ({ signal }) =>
      currentProjectId && token
        ? processService.listInstances(currentProjectId, { page, pageSize }, signal)
        : Promise.resolve({ instances: [], err: '', pageInfo: undefined } as InstancesResult),
    enabled: !!currentProjectId && !!token,
    // Hold the current rows while the next page loads, so the table does not
    // collapse into a skeleton on every click.
    placeholderData: (previous) => previous,
  });
};