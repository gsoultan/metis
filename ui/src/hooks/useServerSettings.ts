import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { serverSettingsService } from '../services/domains/serverSettingsService';
import { errorMessage } from '../services/shared/errors';

const SERVER_SETTINGS_KEY = ['server-settings'] as const;

/** The server's settings. Not cached hard: whoever opens them is about to change one. */
export const useServerSettings = (enabled: boolean) =>
  useQuery({
    queryKey: SERVER_SETTINGS_KEY,
    queryFn: ({ signal }) => serverSettingsService.listServerSettings(signal),
    enabled,
    retry: false,
  });

export const useServerSettingHistory = (name: string | null) =>
  useQuery({
    queryKey: [...SERVER_SETTINGS_KEY, 'history', name],
    queryFn: ({ signal }) => serverSettingsService.serverSettingHistory(name ?? '', signal),
    enabled: name !== null,
  });

export const useChangeServerSetting = (failedTitle: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, value, reason }: { name: string; value: string | null; reason: string }) =>
      serverSettingsService.changeServerSetting(name, value, reason),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: SERVER_SETTINGS_KEY }),
    onError: (error: unknown) => {
      notifications.show({ title: failedTitle, message: errorMessage(error, ''), color: 'red' });
    },
  });
};
