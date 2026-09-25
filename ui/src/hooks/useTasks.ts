import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { TASK_LIST_EVENTS } from '../domain/taskEvents';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ProcessVariables } from '../services/types';
import { useInvalidateOnEvents } from './useEventStream';

type AllTasksResult = Awaited<ReturnType<typeof processService.listTasks>>;

const NO_TASKS: AllTasksResult = { tasks: [], pageInfo: undefined };

/**
 * Every task in the current project, one page at a time.
 *
 * `enabled` lets a screen that only sometimes shows this list — the kanban
 * board — stop fetching every task in the project while it is in table mode.
 */
export const useTasks = (page = 1, pageSize = 50, options: { enabled?: boolean } = {}) => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useQuery({
    queryKey: ['tasks', currentProjectId, page, pageSize],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.listTasks(currentProjectId, { page, pageSize }, signal)
        : Promise.resolve(NO_TASKS),
    enabled: !!currentProjectId && (options.enabled ?? true),
  });
};

/**
 * The current project's open work with a deadline, for the dashboard's report:
 * read on the server across all of it, soonest first, each task naming its
 * process. Refetched when a task changes rather than polled.
 */
export const useDeadlines = () => {
  const { currentProjectId } = useAppStore();
  useInvalidateOnEvents(TASK_LIST_EVENTS, ['deadlines', currentProjectId]);
  return useQuery({
    queryKey: ['deadlines', currentProjectId],
    queryFn: ({ signal }) => processService.deadlines(currentProjectId as string, signal),
    enabled: !!currentProjectId,
  });
};

export const useTasksByAssignee = (assignee: string, page = 1, pageSize = 25) => {
  return useQuery({
    // The page is part of the key, so moving between pages is a cache hit on
    // the way back rather than a refetch.
    queryKey: ['tasks', 'assignee', assignee, page, pageSize],
    queryFn: ({ signal }) =>
      assignee
        ? processService.listTasksByAssignee(assignee, { page, pageSize }, signal)
        : Promise.resolve(NO_TASKS),
    enabled: !!assignee,
    // Keeps the previous page on screen while the next one loads, so the table
    // does not collapse to a skeleton on every page change.
    placeholderData: (previous) => previous,
  });
};

/**
 * The unclaimed tasks the signed-in user could take. Who is asking comes from
 * the token; the user id is in the key only so that one person's list is never
 * served from another's cache.
 */
export const useTasksByCandidates = (page = 1, pageSize = 25) => {
  const user = useAppStore((state) => state.user);
  return useQuery({
    queryKey: ['tasks', 'candidates', user?.id ?? '', page, pageSize],
    queryFn: ({ signal }) => processService.listTasksByCandidates({ page, pageSize }, signal),
    enabled: !!user,
    placeholderData: (previous) => previous,
  });
};

export const useIncidents = (instanceId: string | null) => {
  return useQuery({
    queryKey: ['incidents', instanceId],
    queryFn: ({ signal }) =>
      instanceId ? processService.listIncidents(instanceId, signal) : Promise.resolve({ incidents: [] }),
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
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useMutation({
    mutationFn: ({ definitionKey, variables, version }: { definitionKey: string; variables?: ProcessVariables; version?: number }) =>
      currentProjectId
        ? processService.startProcess(currentProjectId, definitionKey, variables, version)
        : Promise.reject('No project selected'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks', currentProjectId] });
    },
  });
};

export const useCompleteTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, variables, taskName }: { id: string; variables?: ProcessVariables; taskName?: string }) =>
      processService.completeTask(id, variables, undefined, taskName),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      // Queued is not completed. Saying "task completed" for something still
      // sitting on the device would have somebody walk away from work the
      // server has never heard of.
      if (result?.queued) {
        notifications.show({
          title: 'Saved on this device',
          message: "You are offline, so this is kept here and sent as soon as you have a connection. Nothing has reached the server yet.",
          color: 'blue',
        });
        return;
      }
      notifications.show({
        title: 'Task completed',
        message: "It's off your list. The process moves on to its next step, and anything that needs you again will show up in your inbox.",
        color: 'green',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'The task was not completed',
        message: error.message || 'Nothing was changed. Try again, or ask an administrator if it keeps failing.',
        color: 'red',
      });
    }
  });
};

export const useClaimTask = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, taskName }: { id: string; taskName?: string }) =>
      processService.claimTask(id, undefined, taskName),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      if (result?.queued) {
        notifications.show({
          title: 'Saved on this device',
          message: "You are offline, so this is kept here and sent when you have a connection. Somebody else could still claim it first.",
          color: 'blue',
        });
        return;
      }
      notifications.show({
        title: 'Task claimed',
        message: "It's yours now. Find it under Assigned to me.",
        color: 'blue',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Could not claim the task',
        message: error.message || 'Someone else may have taken it first.',
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
        title: 'Task released',
        message: "It's back under Available to claim for anyone in the group.",
        color: 'gray',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Could not release the task',
        message: error.message || 'It is still assigned to you.',
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
        title: 'Task delegated',
        message: 'It now sits in their inbox.',
        color: 'yellow',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Could not delegate the task',
        message: error.message || 'It is still with you.',
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
        title: 'Task updated',
        message: 'The changes are saved.',
        color: 'green',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Could not save the changes',
        message: error.message || 'The task is unchanged.',
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
        title: 'Task reassigned',
        message: 'It now sits in their inbox.',
        color: 'blue',
      });
    },
    onError: (error) => {
      notifications.show({
        title: 'Could not reassign the task',
        message: error.message || 'The assignment is unchanged.',
        color: 'red',
      });
    }
  });
};
