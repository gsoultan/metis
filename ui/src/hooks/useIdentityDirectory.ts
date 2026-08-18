import { useQuery } from '@tanstack/react-query';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';

export const useUsers = (organizationId: string | null) => {
  return useQuery({
    staleTime: DIRECTORY_STALE_TIME,
    queryKey: ['users', organizationId],
    queryFn: ({ signal }) =>
      organizationId ? processService.listUsers(organizationId, signal) : Promise.resolve({ users: [] }),
    enabled: !!organizationId,
  });
};

export const useUserGroups = (userId: string | null) => {
  return useQuery({
    staleTime: DIRECTORY_STALE_TIME,
    queryKey: ['user-groups', userId],
    queryFn: ({ signal }) =>
      userId ? processService.listUserGroups(userId, signal) : Promise.resolve({ groups: [] }),
    enabled: !!userId,
  });
};

export const useGroups = (organizationId: string | null) => {
  return useQuery({
    staleTime: DIRECTORY_STALE_TIME,
    queryKey: ['groups', organizationId],
    queryFn: ({ signal }) =>
      organizationId ? processService.listGroups(organizationId, signal) : Promise.resolve({ groups: [] }),
    enabled: !!organizationId,
  });
};