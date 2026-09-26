import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { processService } from '../services/api';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';

export const useConnectorManifests = () =>
  useQuery({
    queryKey: ['connector-manifests'],
    staleTime: AUTHORED_STALE_TIME,
    queryFn: ({ signal }) => processService.listConnectorManifests(signal),
  });

/**
 * Installing, switching or removing a manifest also adds, rewrites or
 * withdraws its entry in the connector catalogue — the one the Connectors page
 * and the designer offer — so both lists are read again.
 */
const refreshAfterManifestChange = (queryClient: QueryClient) => {
  queryClient.invalidateQueries({ queryKey: ['connector-manifests'] });
  queryClient.invalidateQueries({ queryKey: ['connectors'] });
};

export const useInstallConnectorManifest = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ document, format }: { document: string; format: 'manifest' | 'openapi' }) =>
      processService.installConnectorManifest(document, format),
    onSuccess: () => refreshAfterManifestChange(queryClient),
  });
};

export const useSetConnectorManifestEnabled = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      processService.setConnectorManifestEnabled(id, enabled),
    onSuccess: () => refreshAfterManifestChange(queryClient),
  });
};

export const useDeleteConnectorManifest = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteConnectorManifest(id),
    onSuccess: () => refreshAfterManifestChange(queryClient),
  });
};
