import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { releaseService } from '../services/domains/releaseService';
import { useAppStore } from '../store/useAppStore';

export const releaseKeys = {
  list: (projectId: string | null) => ['releases', projectId] as const,
  runtime: (projectId: string | null) => ['runtime', projectId] as const,
};

/** The current project's releases in this environment, newest first. */
export function useReleases() {
  const projectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  return useQuery({
    queryKey: releaseKeys.list(projectId),
    queryFn: ({ signal }) => releaseService.list(projectId ?? '', signal),
    enabled: !!token && !!projectId,
  });
}

/**
 * Which runtime the page is served from. It changes only when an
 * administrator protects or renames an environment, so it is kept a while.
 */
export function useRuntime() {
  const projectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  return useQuery({
    queryKey: releaseKeys.runtime(projectId),
    queryFn: ({ signal }) => releaseService.runtime(projectId ?? '', signal),
    enabled: !!token && !!projectId,
    staleTime: 5 * 60_000,
  });
}

/** The release actions. Each refreshes the list and what runs. */
export function useReleaseActions() {
  const projectId = useAppStore((state) => state.currentProjectId) ?? '';
  const client = useQueryClient();
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['releases', projectId] });
    void client.invalidateQueries({ queryKey: ['definitions', projectId] });
    void client.invalidateQueries({ queryKey: ['decisions', projectId] });
  };
  return {
    cut: useMutation({
      mutationFn: (v: { name: string; processes: string[]; decisions: string[] }) =>
        releaseService.cut(projectId, v.name, v.processes, v.decisions),
      onSuccess: refresh,
    }),
    importFrom: useMutation({
      mutationFn: (v: { fromEnvironment: string; name: string }) => releaseService.importFrom(projectId, v.fromEnvironment, v.name),
      onSuccess: refresh,
    }),
    approve: useMutation({ mutationFn: (name: string) => releaseService.approve(projectId, name), onSuccess: refresh }),
    goLive: useMutation({ mutationFn: (name: string) => releaseService.goLive(projectId, name), onSuccess: refresh }),
    rollBack: useMutation({ mutationFn: () => releaseService.rollBack(projectId), onSuccess: refresh }),
  };
}
