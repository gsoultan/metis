import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import { reconnectEventStream } from './useEventStream';

export const useOrganizations = () => {
  const token = useAppStore((state) => state.token);

  return useQuery({
    staleTime: DIRECTORY_STALE_TIME,
    queryKey: ['organizations'],
    queryFn: ({ signal }) => token ? processService.listOrganizations(signal) : Promise.resolve({ organizations: [], err: '' }),
    enabled: !!token,
  });
};

export const useCreateOrganization = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, description }: { name: string; description: string }) =>
      processService.createOrganization(name, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['organizations'] });
    },
  });
};

export const useUpdateOrganization = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name, description }: { id: string; name: string; description: string }) =>
      processService.updateOrganization(id, name, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['organizations'] });
    },
  });
};

export const useDeleteOrganization = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteOrganization(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['organizations'] });
    },
  });
};

/**
 * Works in another organization from now on.
 *
 * Everything already read was the previous organization's — its projects, its
 * instances, its people — so every query is read again, and the live event
 * stream is opened again for the new one. The stored choice changes first,
 * because that is what the next request reads to say which organization it is
 * for.
 */
export const useSwitchOrganization = () => {
  const queryClient = useQueryClient();
  const setCurrentOrganizationId = useAppStore((state) => state.setCurrentOrganizationId);
  const setCurrentProjectId = useAppStore((state) => state.setCurrentProjectId);
  return (id: string | null) => {
    if (useAppStore.getState().currentOrganizationId === id) return;
    setCurrentOrganizationId(id);
    // The previous project belongs to the previous organization.
    setCurrentProjectId(null);
    reconnectEventStream();
    void queryClient.invalidateQueries();
  };
};
