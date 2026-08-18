import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

export const useOrganizations = () => {
  const { token } = useAppStore();

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
