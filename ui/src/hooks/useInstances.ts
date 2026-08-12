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

export const useInstances = () => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    queryKey: ['instances', currentProjectId],
    queryFn: ({ signal }) =>
      (currentProjectId && token) ? processService.listInstances(currentProjectId, signal) : Promise.resolve({ instances: [], err: "" }),
    enabled: !!currentProjectId && !!token,
  });
};