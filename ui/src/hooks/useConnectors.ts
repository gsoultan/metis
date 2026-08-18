import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ApiConnector, ApiConnectorInstance, CreateConnectorInstancePayload, CreateConnectorPayload } from '../services/types';

export const useConnectors = () => {
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['connectors'],
    queryFn: ({ signal }) => processService.listConnectors(signal),
  });
};

export const useCreateConnector = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (connector: CreateConnectorPayload) => processService.createConnector(connector),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connectors'] });
    },
  });
};

export const useUpdateConnector = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (connector: ApiConnector) => processService.updateConnector(connector),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connectors'] });
    },
  });
};

export const useDeleteConnector = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteConnector(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connectors'] });
    },
  });
};

export const useConnectorInstances = () => {
  const { currentProjectId } = useAppStore();
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['connector-instances', currentProjectId],
    queryFn: ({ signal }) =>
      currentProjectId ? processService.listConnectorInstances(currentProjectId, signal) : Promise.resolve({ instances: [], err: "" }),
    enabled: !!currentProjectId,
  });
};

export const useCreateConnectorInstance = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (instance: CreateConnectorInstancePayload) => processService.createConnectorInstance(instance),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connector-instances', currentProjectId] });
    },
  });
};

export const useUpdateConnectorInstance = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (instance: ApiConnectorInstance) => processService.updateConnectorInstance(instance),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connector-instances', currentProjectId] });
    },
  });
};

export const useDeleteConnectorInstance = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (id: string) => processService.deleteConnectorInstance(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connector-instances', currentProjectId] });
    },
  });
};

export const useExecuteConnector = () => {
  return useMutation({
    mutationFn: ({ connectorKey, config, payload }: { connectorKey: string; config: Record<string, unknown>; payload: Record<string, unknown> }) =>
      processService.executeConnector(connectorKey, config, payload),
  });
};

export const useExecuteScript = () => {
  return useMutation({
    mutationFn: ({ script, scriptFormat, variables }: { script: string; scriptFormat: string; variables: Record<string, unknown> }) =>
      processService.executeScript(script, scriptFormat, variables),
  });
};