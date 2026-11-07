import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { approvalLineService } from '../services/domains/approvalLineService';
import { RequestError } from '../services/shared/rest';
import { errorMessage } from '../services/shared/errors';
import { moveSummary } from '../domain/approvalTakeover';
import { useAppStore } from '../store/useAppStore';

/**
 * The open requests still on an approval line's older versions. A process
 * that is not an approval line answers 404, and somebody who is not an
 * administrator 403: both read as nothing to show, not as an error.
 */
export const useApprovalsBehind = (processKey: string | null) => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  return useQuery({
    queryKey: ['approval-lines', currentProjectId, processKey, 'behind'],
    queryFn: async ({ signal }) => {
      try {
        return await approvalLineService.listApprovalsBehind(currentProjectId!, processKey!, signal);
      } catch (error) {
        if (error instanceof RequestError && (error.status === 404 || error.status === 403)) return null;
        throw error;
      }
    },
    enabled: !!currentProjectId && !!token && !!processKey,
    retry: false,
  });
};

/** Moves chosen requests onto the line's live version. */
export const useMoveApprovals = (processKey: string | null, liveVersion: number) => {
  const queryClient = useQueryClient();
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useMutation({
    mutationFn: (requests: string[]) => {
      if (!currentProjectId || !processKey) throw new Error('No project selected');
      return approvalLineService.moveApprovals(currentProjectId, processKey, requests);
    },
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['approval-lines', currentProjectId, processKey] });
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId, processKey] });
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      const left = result.left?.length ?? 0;
      notifications.show({
        title: result.moved > 0 ? 'Requests moved' : 'Nothing moved',
        message: moveSummary(result.moved, left, liveVersion),
        color: left ? 'yellow' : 'teal',
      });
    },
    onError: (error: unknown) => {
      notifications.show({ title: 'Nothing moved', message: errorMessage(error, 'The requests are where they were.'), color: 'red' });
    },
  });
};
