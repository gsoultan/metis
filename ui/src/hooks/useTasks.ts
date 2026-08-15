import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ProcessVariables } from '../services/types';

type AllTasksResult = Awaited<ReturnType<typeof processService.listTasks>>;

export const useTasks = (page = 1, pageSize = 50) => {
  const { currentProjectId } = useAppStore();
  return useQuery({
    queryKey: ['tasks', currentProjectId, page, pageSize],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.listTasks(currentProjectId, { page, pageSize }, signal)
        : Promise.resolve({ tasks: [], err: '', pageInfo: undefined } as AllTasksResult),
    enabled: !!currentProjectId,
  });
};

type AssigneeTasksResult = Awaited<ReturnType<typeof processService.listTasksByAssignee>>;

export const useTasksByAssignee = (assignee: string, page = 1, pageSize = 25) => {
  return useQuery({
    // The page is part of the key, so moving between pages is a cache hit on
    // the way back rather than a refetch.
    queryKey: ['tasks', 'assignee', assignee, page, pageSize],
    queryFn: ({ signal }) =>
      assignee
        ? processService.listTasksByAssignee(assignee, { page, pageSize }, signal)
        : Promise.resolve({ tasks: [], err: '', pageInfo: undefined } as AssigneeTasksResult),
    enabled: !!assignee,
    // Keeps the previous page on screen while the next one loads, so the table
    // does not collapse to a skeleton on every page change.
    placeholderData: (previous) => previous,
  });
};

type CandidateTasksResult = Awaited<ReturnType<typeof processService.listTasksByCandidates>>;

export const useTasksByCandidates = (userId: string, groups: string[] = [], page = 1, pageSize = 25) => {
  return useQuery({
    queryKey: ['tasks', 'candidates', userId, groups.join(','), page, pageSize],
    queryFn: ({ signal }) =>
      userId
        ? processService.listTasksByCandidates(userId, groups, { page, pageSize }, signal)
        : Promise.resolve({ tasks: [], err: '', pageInfo: undefined } as CandidateTasksResult),
    enabled: !!userId,
    placeholderData: (previous) => previous,
  });
};

export const useIncidents = (instanceId: string | null) => {
  return useQuery({
    queryKey: ['incidents', instanceId],
    queryFn: ({ signal }) =>
      instanceId ? processService.listIncidents(instanceId, signal) : Promise.resolve({ incidents: [], err: "" }),
    enabled: !!instanceId,
  });
};

export const useResolveIncident = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.resolveIncident(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['incidents'] });
    },
  });
};

export const useStartProcess = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ definitionKey, variables }: { definitionKey: string; variables?: ProcessVariables }) =>
      currentProjectId ? processService.startProcess(currentProjectId, definitionKey, variables) : Promise.reject('No project selected'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks', currentProjectId] });
    },
  });
};

export const useCompleteTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, userId, variables }: { id: string; userId: string; variables?: ProcessVariables }) => processService.completeTask(id, userId, variables),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Completed',
        message: 'The task has been successfully completed.',
        color: 'green',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to complete task',
        color: 'red',
      });
    }
  });
};

export const useClaimTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, userId }: { id: string; userId: string }) => processService.claimTask(id, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Claimed',
        message: 'The task has been successfully claimed.',
        color: 'blue',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to claim task',
        color: 'red',
      });
    }
  });
};

export const useUnclaimTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.unclaimTask(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Unclaimed',
        message: 'The task has been successfully unclaimed.',
        color: 'gray',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to unclaim task',
        color: 'red',
      });
    }
  });
};

export const useDelegateTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, userId }: { id: string; userId: string }) => processService.delegateTask(id, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Delegated',
        message: 'The task has been successfully delegated.',
        color: 'yellow',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to delegate task',
        color: 'red',
      });
    }
  });
};

export const useUpdateTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name, priority, dueDate }: { id: string; name: string; priority: number; dueDate?: string }) =>
      processService.updateTask(id, name, priority, dueDate),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Updated',
        message: 'The task has been successfully updated.',
        color: 'green',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to update task',
        color: 'red',
      });
    }
  });
};

export const useAssignTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, userId }: { id: string; userId: string }) => processService.assignTask(id, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      notifications.show({
        title: 'Task Assigned',
        message: 'The task has been successfully assigned.',
        color: 'blue',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Error',
        message: error.message || 'Failed to assign task',
        color: 'red',
      });
    }
  });
};