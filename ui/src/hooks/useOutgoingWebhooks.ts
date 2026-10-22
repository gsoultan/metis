import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  outgoingWebhookService,
  type CreateOutgoingWebhookPayload,
  type DeliveryStatus,
  type OutgoingWebhookEvent,
} from '../services/domains/outgoingWebhookService';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';
import { useAppStore } from '../store/useAppStore';

const LIST_KEY = 'outgoing-webhooks';
const LOG_KEY = 'outgoing-webhook-deliveries';

export const useOutgoingWebhooks = () => {
  const { currentProjectId } = useAppStore();
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: [LIST_KEY, currentProjectId],
    queryFn: ({ signal }) => outgoingWebhookService.listOutgoingWebhooks(currentProjectId ?? '', signal),
    enabled: !!currentProjectId,
  });
};

export const useCreateOutgoingWebhook = () => {
  const { currentProjectId } = useAppStore();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: CreateOutgoingWebhookPayload) =>
      outgoingWebhookService.createOutgoingWebhook(currentProjectId ?? '', payload),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [LIST_KEY] }),
  });
};

export const useSetOutgoingWebhookEnabled = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      outgoingWebhookService.setOutgoingWebhookEnabled(id, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [LIST_KEY] }),
  });
};

export const useDeleteOutgoingWebhook = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => outgoingWebhookService.deleteOutgoingWebhook(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: [LIST_KEY] }),
  });
};

/** A test is logged like any delivery, so the log is read again once it is in. */
export const useSendOutgoingWebhookTest = (webhookId: string) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (event?: OutgoingWebhookEvent) => outgoingWebhookService.sendOutgoingWebhookTest(webhookId, event),
    onSettled: () => queryClient.invalidateQueries({ queryKey: [LOG_KEY, webhookId] }),
  });
};

/** A webhook's log, newest first, a page at a time. */
export const useOutgoingDeliveries = (webhookId: string | null, status?: DeliveryStatus) =>
  useInfiniteQuery({
    queryKey: [LOG_KEY, webhookId, status ?? 'all'],
    queryFn: ({ pageParam, signal }) =>
      outgoingWebhookService.listOutgoingDeliveries(webhookId ?? '', { status, before: pageParam }, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_before,
    enabled: !!webhookId,
  });

const useDeliveryAction = (webhookId: string, act: (deliveryId: string) => Promise<void>) => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: act,
    onSettled: () => queryClient.invalidateQueries({ queryKey: [LOG_KEY, webhookId] }),
  });
};

export const useRetryOutgoingDelivery = (webhookId: string) =>
  useDeliveryAction(webhookId, (id) => outgoingWebhookService.retryOutgoingDelivery(id));

export const useStopOutgoingDelivery = (webhookId: string) =>
  useDeliveryAction(webhookId, (id) => outgoingWebhookService.stopOutgoingDelivery(id));
