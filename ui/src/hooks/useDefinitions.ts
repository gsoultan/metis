import { notifications } from '@mantine/notifications';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ApiNodeAction, CreateDefinitionPayload } from '../services/types';

// The queryFn ternary returned the service's real result on one branch and a
// hand-written literal on the other. TypeScript widened that union to `{}`,
// so every property access on the result failed once the processService
// facade stopped being typed `any`. Deriving the fallback from the service's
// own signature keeps both branches the same shape.
type DefinitionsResult = Awaited<ReturnType<typeof processService.listDefinitions>>;
type DefinitionResult = Awaited<ReturnType<typeof processService.getDefinition>>;
type DefinitionVersionsResult = Awaited<ReturnType<typeof processService.listDefinitionVersions>>;
type LiveVersionsResult = Awaited<ReturnType<typeof processService.listLiveVersions>>;

export const useDefinitions = (page = 1, pageSize = 25) => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
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
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['definition', currentProjectId, id],
    queryFn: ({ signal }) =>
      (currentProjectId && id && token)
        ? processService.getDefinition(currentProjectId, id, signal)
        : Promise.resolve({ definition: undefined, err: '' } as DefinitionResult),
    enabled: !!currentProjectId && !!id && !!token,
  });
};

/**
 * Deploys a version, optionally without making it live.
 *
 * `stage` is what turns a deploy into a two-step cutover: the model is saved and
 * numbered, but new instances keep starting on the version that is live now.
 * Nothing here affects instances already running — those finish on the version
 * they started on, always.
 */
export const useCreateDefinition = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ definition, stage }: { definition: CreateDefinitionPayload; stage?: boolean }) =>
      currentProjectId
        ? processService.createDefinition(currentProjectId, definition, { stage })
        : Promise.reject('No project selected'),
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: ['definitions', currentProjectId] });
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId, variables.definition.key] });
      queryClient.invalidateQueries({ queryKey: ['liveVersions', currentProjectId] });
    },
  });
};

/**
 * One process key's version history: which version is live, and how much work
 * each one still holds.
 *
 * Refetched rather than cached hard, because the interesting number — how many
 * instances an old version still has running — is the one that changes on its
 * own as work finishes.
 */
export const useDefinitionVersions = (key: string | null) => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    queryKey: ['definitionVersions', currentProjectId, key],
    queryFn: ({ signal }) =>
      (currentProjectId && key && token)
        ? processService.listDefinitionVersions(currentProjectId, key, signal)
        : Promise.resolve({ versions: [], err: '' } as DefinitionVersionsResult),
    enabled: !!currentProjectId && !!key && !!token,
  });
};

/**
 * Arranges for a version to take over at a chosen time.
 *
 * Invalidates the same keys a promotion does. The cutover itself needs no
 * client-side timer: the server resolves the live version from the release
 * timeline and the clock, so a page opened after the time has passed simply
 * reads the new answer.
 */
export const useScheduleDefinitionVersion = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ key, version, activateAt }: { key: string; version: number; activateAt: Date }) =>
      currentProjectId
        ? processService.scheduleDefinitionVersion(currentProjectId, key, version, activateAt)
        : Promise.reject('No project selected'),
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId, variables.key] });
      queryClient.invalidateQueries({ queryKey: ['liveVersions', currentProjectId] });
    },
  });
};

/** Drops a cutover that has not happened yet. */
export const useCancelScheduledVersion = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ releaseId }: { key: string; releaseId: string }) =>
      currentProjectId
        ? processService.cancelScheduledVersion(currentProjectId, releaseId)
        : Promise.reject('No project selected'),
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId, variables.key] });
      queryClient.invalidateQueries({ queryKey: ['liveVersions', currentProjectId] });
    },
  });
};

/**
 * Which version of each process key is live, for the list page.
 *
 * Separate from the definitions list because the two answer different
 * questions: the list says what exists, this says what will actually run.
 */
export const useLiveVersions = () => {
  const { currentProjectId, token } = useAppStore();
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['liveVersions', currentProjectId],
    queryFn: ({ signal }) =>
      (currentProjectId && token)
        ? processService.listLiveVersions(currentProjectId, signal)
        : Promise.resolve({ live: {}, err: '' } as LiveVersionsResult),
    enabled: !!currentProjectId && !!token,
  });
};

/** Makes one deployed version the one new instances start on. */
export const usePromoteDefinitionVersion = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ key, version }: { key: string; version: number }) =>
      currentProjectId
        ? processService.promoteDefinitionVersion(currentProjectId, key, version)
        : Promise.reject('No project selected'),
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: ['definitions', currentProjectId] });
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId, variables.key] });
      queryClient.invalidateQueries({ queryKey: ['liveVersions', currentProjectId] });
    },
  });
};

/**
 * Moving running instances onto another version.
 *
 * Two mutations rather than one with a flag: previewing changes nothing and must
 * not invalidate anything, while applying rewrites instances, tasks and jobs and
 * has to refresh all three. One mutation with a `dryRun` argument would make the
 * cache invalidation conditional on an argument, which is how a preview comes to
 * blank somebody's task list.
 */
interface MigrationArgs {
  source: string;
  target: string;
  mapping: Record<string, string>;
  /** Control-bearing steps the operator has accepted losing, by node id. */
  acknowledge?: string[];
  /** Nodes whose work is decided rather than moved. */
  actions?: Record<string, ApiNodeAction>;
}

export const usePlanInstanceMigration = () => {
  return useMutation({
    mutationFn: ({ source, target, mapping, acknowledge, actions }: MigrationArgs) =>
      processService.migrateInstances(source, target, mapping, true, acknowledge ?? [], actions ?? {}),
  });
};

export const useMigrateInstances = () => {
  const queryClient = useQueryClient();
  const { currentProjectId } = useAppStore();
  return useMutation({
    mutationFn: ({ source, target, mapping, acknowledge, actions }: MigrationArgs) =>
      processService.migrateInstances(source, target, mapping, false, acknowledge ?? [], actions ?? {}),
    onSuccess: () => {
      // Instances, the inbox and the version history all change: a task that was
      // on one node is now on another, and the instance names a different
      // version. Leaving any of them cached shows work where it no longer is.
      queryClient.invalidateQueries({ queryKey: ['instances', currentProjectId] });
      queryClient.invalidateQueries({ queryKey: ['tasks', currentProjectId] });
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId] });
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
      // The version history and the list page's live map both name versions by
      // number, so removing one has to refresh them or they keep offering it.
      queryClient.invalidateQueries({ queryKey: ['definitionVersions', currentProjectId] });
      queryClient.invalidateQueries({ queryKey: ['liveVersions', currentProjectId] });
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
    mutationFn: (xml: string) => {
      // Refused here rather than sent: the server would refuse it anyway, with
      // a message about UUIDs that tells the person nothing.
      if (!currentProjectId) {
        throw new Error('Choose a project to import the model into.');
      }
      return processService.importDefinition(currentProjectId, xml);
    },
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