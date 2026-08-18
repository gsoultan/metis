import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { processService } from '../services/api';
import type { CreateWebhookPayload } from '../services/domains/webhookService';
import { useAppStore } from '../store/useAppStore';

export const useWebhooks = () => {
  const { currentProjectId } = useAppStore();
  return useQuery({
    queryKey: ['webhooks', currentProjectId],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.listWebhooks(currentProjectId, signal)
        : Promise.resolve({ webhooks: [], err: undefined }),
    enabled: !!currentProjectId,
  });
};

export const useCreateWebhook = () => {
  const { currentProjectId } = useAppStore();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (params: Omit<CreateWebhookPayload, 'project_id'>) =>
      processService.createWebhook({ ...params, project_id: currentProjectId ?? '' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['webhooks'] }),
  });
};

export const useSetWebhookEnabled = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      processService.setWebhookEnabled(id, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['webhooks'] }),
  });
};

export const useDeleteWebhook = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteWebhook(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['webhooks'] }),
  });
};
