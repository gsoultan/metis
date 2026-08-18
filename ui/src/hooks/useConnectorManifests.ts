import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { processService } from '../services/api';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';

export const useConnectorManifests = () =>
  useQuery({
    queryKey: ['connector-manifests'],
    staleTime: AUTHORED_STALE_TIME,
    queryFn: ({ signal }) => processService.listConnectorManifests(signal),
  });

export const useInstallConnectorManifest = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ document, format }: { document: string; format: 'manifest' | 'openapi' }) =>
      processService.installConnectorManifest(document, format),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connector-manifests'] });
      // A manifest can replace a built-in, so the connector catalogue the
      // designer offers has changed too.
      queryClient.invalidateQueries({ queryKey: ['connectors'] });
    },
  });
};

export const useSetConnectorManifestEnabled = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      processService.setConnectorManifestEnabled(id, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['connector-manifests'] }),
  });
};

export const useDeleteConnectorManifest = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteConnectorManifest(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['connector-manifests'] }),
  });
};
