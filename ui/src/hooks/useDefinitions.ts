import { notifications } from '@mantine/notifications';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

// The queryFn ternary returned the service's real result on one branch and a
// hand-written literal on the other. TypeScript widened that union to `{}`,
// so every property access on the result failed once the processService
// facade stopped being typed `any`. Deriving the fallback from the service's
// own signature keeps both branches the same shape.
type DefinitionsResult = Awaited<ReturnType<typeof processService.listDefinitions>>;
type DefinitionResult = Awaited<ReturnType<typeof processService.getDefinition>>;

export const useDefinitions = () => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    queryKey: ['definitions', currentProjectId],
    queryFn: ({ signal }) =>
      (currentProjectId && token)
        ? processService.listDefinitions(currentProjectId, signal)
        : Promise.resolve({ definitions: [], err: '' } as DefinitionsResult),
    enabled: !!currentProjectId && !!token,
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
    mutationFn: (definition: any) =>
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
    onError: (error: any) => {
      notifications.show({
        title: 'Import Error',
        message: error.message || 'Failed to import BPMN model.',
        color: 'red',
      });
    }
  });
};