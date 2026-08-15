import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { CreateDefinitionPayload } from '../services/types';

// The queryFn ternary returned the service's real result on one branch and a
// hand-written literal on the other. TypeScript widened that union to `{}`,
// so every property access on the result failed once the processService
// facade stopped being typed `any`. Deriving the fallback from the service's
// own signature keeps both branches the same shape.
type DefinitionsResult = Awaited<ReturnType<typeof processService.listDefinitions>>;
type DefinitionResult = Awaited<ReturnType<typeof processService.getDefinition>>;

export const useDefinitions = (page = 1, pageSize = 25) => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    // The page is part of the key, so stepping back to a page already seen is
    // a cache hit rather than a refetch.
    queryKey: ['definitions', currentProjectId, page, pageSize],
    queryFn: ({ signal }) =>
      (currentProjectId && token)
        ? processService.listDefinitions(currentProjectId, { page, pageSize }, signal)
        : Promise.resolve({ definitions: [], err: '', pageInfo: undefined } as DefinitionsResult),
    enabled: !!currentProjectId && !!token,
    // Hold the current rows while the next page loads, so the list does not
    // collapse into a skeleton on every click.
    placeholderData: (previous) => previous,
  });
};

export const useDefinition = (id: string | null) => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    queryKey: ['definition', currentProjectId, id],
    queryFn: ({ signal }) =>
      (currentProjectId && id && token)
        ? processService.getDefinition(currentProjectId, id, signal)
        : Promise.resolve({ definition: undefined, err: '' } as DefinitionResult),
    enabled: !!currentProjectId && !!id && !!token,
  });
};

export const useCreateDefinition = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (definition: CreateDefinitionPayload) =>
      currentProjectId ? processService.createDefinition(currentProjectId, definition) : Promise.reject('No project selected'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['definitions', currentProjectId] });
    },
  });
};

export const useDeleteDefinition = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (id: string) => processService.deleteDefinition(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['definitions', currentProjectId] });
    },
  });
};

export const useExportDefinition = () => {
  return useMutation({
    mutationFn: (id: string) => processService.exportDefinition(id),
  });
};

export const useImportDefinition = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: (xml: string) => processService.importDefinition(xml),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['definitions', currentProjectId] });
      notifications.show({
        title: 'Success',
        message: 'BPMN model imported successfully.',
        color: 'teal',
      });
    },
    onError: (error: unknown) => {
      notifications.show({
        title: 'Import Error',
        message: (error instanceof Error ? error.message : 'Failed to import BPMN model.'),
        color: 'red',
      });
    }
  });
};