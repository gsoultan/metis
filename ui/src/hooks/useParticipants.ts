import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ImportSummary } from '../domain/participantImport';
import { errorMessage } from '../services/shared/errors';

type ParticipantsResult = Awaited<ReturnType<typeof processService.listParticipants>>;

/**
 * The people this project's processes can assign work to.
 *
 * `limit` asks for at most that many, under a key of its own beneath the
 * project's, so an import or a removal refreshes it with the whole list.
 */
export const useParticipants = (options: { limit?: number } = {}) => {
  const { currentProjectId, token } = useAppStore();
  const { limit } = options;
  return useQuery({
    queryKey: limit ? ['participants', currentProjectId, { limit }] : ['participants', currentProjectId],
    queryFn: ({ signal }) =>
      currentProjectId && token
        ? processService.listParticipants(currentProjectId, signal, { limit })
        : Promise.resolve({ participants: [], err: '' } as ParticipantsResult),
    enabled: !!currentProjectId && !!token,
  });
};

/**
 * Brings a directory in from a file, an API or a query.
 *
 * The reply is a summary rather than a bare success, because a partial import
 * is the normal case: the good rows land and the rest are reported. Treating it
 * as success-or-failure would throw away the half that says which rows did not.
 */
export const useImportParticipants = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation<ImportSummary, Error, Record<string, unknown>>({
    mutationFn: (source) =>
      currentProjectId
        ? processService.importParticipants(currentProjectId, source)
        : Promise.reject(new Error('No project selected')),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['participants', currentProjectId] });
    },
    onError: (error: unknown) => {
      notifications.show({
        title: 'The directory could not be read',
        message: errorMessage(error, 'Nothing was imported.'),
        color: 'red',
      });
    },
  });
};

/**
 * Takes somebody out of a project's directory.
 *
 * Reversible, and the confirmation says so: the row is marked rather than
 * destroyed and keeps its key, so importing a directory that names them again
 * brings back the same person with their group memberships. Their open tasks
 * are untouched either way — a task names its assignee rather than referencing
 * them, so work in an inbox does not disappear because somebody left.
 */
export const useRemoveParticipant = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation<void, Error, string>({
    mutationFn: async (id) => {
      if (!currentProjectId) throw new Error('No project selected');
      await processService.removeParticipant(currentProjectId, id);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['participants', currentProjectId] });
    },
    onError: (error: unknown) => {
      notifications.show({
        title: 'They could not be removed',
        message: errorMessage(error, 'The directory is unchanged.'),
        color: 'red',
      });
    },
  });
};
