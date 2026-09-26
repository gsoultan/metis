import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { processService } from '../services/api';
import type { OwnProfileUpdate, UserUpdate } from '../services/domains/identityService';
import { useAppStore } from '../store/useAppStore';

export const useUsers = () => {
  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const organizationId = currentOrganizationId || '';
  return useQuery({
    queryKey: ['users', organizationId],
    queryFn: ({ signal }) => processService.listUsers(organizationId, signal),
    enabled: !!organizationId,
  });
};

export const useCreateUser = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (params: { organization_id: string; username: string; password: string; full_name: string; display_name: string; organization: string; email: string; roles: string[] }) =>
      processService.createUser(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });
};

export const useUpdateUser = () => {
  const queryClient = useQueryClient();
  return useMutation({
    // `organization` is accepted because the user list's edit dialog still
    // collects it, and not sent: the server reads memberships, not a name.
    mutationFn: ({ id, organization: _organization, ...user }: { id: string; organization?: string } & UserUpdate) =>
      processService.updateUser(id, user),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });
};

// Keyed by the signed-in account as well: signing out does not clear the
// cache, and a form started from somebody else's profile would save their
// email into the next person's account.
export const useOwnProfile = () => {
  const userId = useAppStore((state) => state.user?.id ?? '');
  return useQuery({
    queryKey: ['own-profile', userId],
    queryFn: ({ signal }) => processService.getOwnProfile(signal),
    enabled: !!userId,
  });
};

export const useUpdateOwnProfile = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (profile: OwnProfileUpdate) => processService.updateOwnProfile(profile),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['own-profile'] });
      // The name is in the account list too.
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });
};

// Nothing is invalidated on success: a password is not part of any cached
// query, and re-fetching the user list after one changes theirs would be
// noise.
export const useChangeOwnPassword = () => {
  return useMutation({
    mutationFn: ({ currentPassword, newPassword }: { currentPassword: string; newPassword: string }) =>
      processService.changeOwnPassword(currentPassword, newPassword),
  });
};

export const useDeleteUser = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
  });
};

export const useGroups = () => {
  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const organizationId = currentOrganizationId || '';
  return useQuery({
    queryKey: ['groups', organizationId],
    queryFn: ({ signal }) => processService.listGroups(organizationId, signal),
    enabled: !!organizationId,
  });
};

export const useCreateGroup = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (params: { organization_id: string; name: string; description: string; roles?: string[] }) =>
      processService.createGroup(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });
};

export const useUpdateGroup = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...group }: { id: string; name: string; description: string; roles?: string[] }) =>
      processService.updateGroup(id, group),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });
};

export const useDeleteGroup = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteGroup(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });
};

export const useGroupMembers = (groupId: string) => {
  return useQuery({
    queryKey: ['group-members', groupId],
    queryFn: ({ signal }) => processService.listGroupMembers(groupId, signal),
    enabled: !!groupId,
  });
};

export const useAddMembership = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
      processService.addMembership(groupId, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-members'] });
    },
  });
};

export const useRemoveMembership = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
      processService.removeMembership(groupId, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-members'] });
    },
  });
};
