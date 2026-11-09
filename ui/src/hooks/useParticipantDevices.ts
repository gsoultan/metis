import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { processService } from '../services/api';
import { errorMessage } from '../services/shared/errors';
import { useAppStore } from '../store/useAppStore';

/**
 * How many push devices each person in the project has. Only read for
 * somebody who manages the directory: the server refuses everybody else.
 */
export const useDeviceCounts = (enabled: boolean) => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  return useQuery({
    queryKey: ['participants', currentProjectId, 'device-counts'],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.deviceCounts(currentProjectId, signal)
        : Promise.resolve<Record<string, number>>({}),
    enabled: enabled && !!currentProjectId && !!token,
  });
};

/** One person's push devices, read when their device list is opened. */
export const useParticipantDevices = (participantId: string | null) => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  return useQuery({
    queryKey: ['participants', currentProjectId, 'devices', participantId],
    queryFn: ({ signal }) =>
      currentProjectId && participantId
        ? processService.listParticipantDevices(currentProjectId, participantId, signal)
        : Promise.resolve([]),
    enabled: !!currentProjectId && !!token && !!participantId,
  });
};

/** Removes a person's device, so pushes stop going to it. */
export const useRemoveParticipantDevice = () => {
  const queryClient = useQueryClient();
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useMutation({
    mutationFn: ({ participantId, deviceId }: { participantId: string; deviceId: string }) => {
      if (!currentProjectId) throw new Error('No project selected');
      return processService.removeParticipantDevice(currentProjectId, participantId, deviceId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['participants', currentProjectId, 'devices'] });
      queryClient.invalidateQueries({ queryKey: ['participants', currentProjectId, 'device-counts'] });
      notifications.show({ title: 'Removed', message: 'Pushes no longer go to that device.', color: 'teal' });
    },
    onError: (error: unknown) => {
      notifications.show({ title: 'Not removed', message: errorMessage(error, 'The device is still registered.'), color: 'red' });
    },
  });
};
