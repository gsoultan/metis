/**
 * A process's saved simulation cases.
 *
 * Kept on the server by process key, beside the simulation endpoint that runs
 * them, so a case somebody builds is there for the next person to open the
 * process — and for the next version of it.
 */
import type { WireCase } from '../../domain/simulationCase';
import { requestJSON } from '../shared/rest';

type SavedCase = WireCase & { id: string };

function casesPath(projectId: string, processKey: string): string {
  return `/projects/${encodeURIComponent(projectId)}/simulation-cases/${encodeURIComponent(processKey)}`;
}

export async function listSimulationCases(projectId: string, processKey: string, signal?: AbortSignal): Promise<SavedCase[]> {
  const reply = await requestJSON<{ cases?: SavedCase[] }>(casesPath(projectId, processKey), { signal });
  return reply.cases ?? [];
}

/** Creates the case when it has no saved id, and rewrites it otherwise. */
export async function saveSimulationCase(
  projectId: string,
  processKey: string,
  savedId: string | undefined,
  body: WireCase,
): Promise<SavedCase> {
  const path = savedId === undefined ? casesPath(projectId, processKey) : `${casesPath(projectId, processKey)}/${encodeURIComponent(savedId)}`;
  const reply = await requestJSON<{ case: SavedCase }>(path, {
    method: savedId === undefined ? 'POST' : 'PUT',
    body: { name: body.name, scenario: body.scenario, expect: body.expect },
  });
  return reply.case;
}

export async function deleteSimulationCase(projectId: string, processKey: string, savedId: string): Promise<void> {
  await requestJSON(`${casesPath(projectId, processKey)}/${encodeURIComponent(savedId)}`, { method: 'DELETE' });
}
