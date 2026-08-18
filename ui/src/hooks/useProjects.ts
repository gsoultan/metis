import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

export const useProjects = (organizationId: string | null) => {
  const { token } = useAppStore();

  return useQuery({
    staleTime: DIRECTORY_STALE_TIME,
    queryKey: ['projects', organizationId],
    queryFn: ({ signal }) =>
      (organizationId && token) ? processService.listProjects(organizationId, signal) : Promise.resolve({ projects: [], err: "" }),
    enabled: !!organizationId && !!token,
  });
};

export const useCreateProject = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ organizationId, name, description }: { organizationId: string; name: string; description: string }) =>
      processService.createProject(organizationId, name, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
};

export const useUpdateProject = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectId, organizationId, name, description }: { projectId: string; organizationId: string; name: string; description: string }) =>
      processService.updateProject(projectId, organizationId, name, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
};

export const useDeleteProject = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (projectId: string) => processService.deleteProject(projectId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
    },
  });
};