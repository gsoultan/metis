/**
 * The processes the sandbox can drive, and what the chosen one expects.
 *
 * Two reads that belong together: the picker's rows, and the model behind the
 * row that is picked. Kept out of the page so the page is composition, and kept
 * in one hook because the second read depends on the first — the definition id
 * is not something the caller has, only the key it chose.
 */
import { useMemo } from 'react';

import type { ProcessChoice } from '../components/sdk';
import { versionsByKey } from '../domain/definitionVersions';
import { readIntegrationSurface, type IntegrationSurface } from '../domain/sdkSurface';
import { useDefinition, useDefinitions } from './useProcess';

interface SandboxProcesses {
  choices: ProcessChoice[];
  loadingChoices: boolean;
  /** Null until a process is chosen, or if its model could not be read. */
  surface: IntegrationSurface | null;
  loadingSurface: boolean;
}

/**
 * @param definitionKey the process chosen, or '' for none
 * @param version 0 for the live version, or a specific one
 */
export function useSandboxProcesses(definitionKey: string, version: number): SandboxProcesses {
  // A run drives one process at a time, so the first page is the whole picker.
  // Asking for more would be asking the server for rows this screen has
  // nowhere to put.
  const definitions = useDefinitions(1, 100);
  const rows = definitions.data?.definitions;

  const choices = useMemo<ProcessChoice[]>(
    () =>
      Object.entries(versionsByKey(rows ?? [])).map(([key, versions]) => ({
        key,
        name: versions[0]?.name ?? key,
        versions: versions.map((definition) => definition.version),
      })),
    [rows],
  );

  // The surface is "what your code has to implement", so it is read from the
  // version instances will actually run: the newest, unless somebody
  // deliberately named an older one.
  const definitionId = useMemo(() => {
    if (definitionKey === '') {
      return null;
    }
    const versions = (rows ?? [])
      .filter((definition) => definition.key === definitionKey)
      .sort((a, b) => b.version - a.version);
    const wanted = version > 0 ? versions.find((candidate) => candidate.version === version) : versions[0];
    return wanted?.id ?? null;
  }, [rows, definitionKey, version]);

  const definition = useDefinition(definitionId);
  const nodes = definition.data?.definition?.nodes;
  const surface = useMemo(() => (nodes === undefined ? null : readIntegrationSurface(nodes)), [nodes]);

  return {
    choices,
    loadingChoices: definitions.isLoading,
    surface,
    loadingSurface: definitionId !== null && definition.isLoading,
  };
}
