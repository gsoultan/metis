/**
 * The simulation client.
 *
 * Over the public REST API rather than Connect, and that is not an accident:
 * this is the same endpoint the Go SDK calls, so a scenario that passes in the
 * browser is the scenario CI runs. If the two spoke different transports, the
 * "run this in CI" snippet beside every scenario would be a guess.
 *
 * A run that ends in an incident is a *result*, not an error. The server
 * answers 200 with `outcome: "incident"` because the engine refusing a
 * decision point is the simulation working correctly — it is the finding
 * somebody came here for. Only a transport failure throws.
 */
import type { SimulationRequest } from '../../domain/simulationScenario';
import { SIMULATION_PATH } from '../../domain/simulationScenario';
import type { SimulationRun } from '../../domain/simulationTrace';
import { API_BASE_URL } from '../shared/config';
import { requestJSON } from '../shared/rest';
import type { ProcessVariables } from '../types';

/**
 * The trace as the server sends it, in the wire's snake_case.
 *
 * Variables are typed as `ProcessVariables` rather than `Record<string,
 * unknown>` for the reason that alias gives: they must survive a JSON round
 * trip, and saying so here rejects a value that could not at the boundary that
 * built it rather than at the transport.
 */
interface WireRun {
  run_id: string;
  definition: { key: string; version: number; id: string };
  outcome: SimulationRun['outcome'];
  ended_at_node: string | null;
  /** Set when `outcome` is `needs_answer`: the step the run stopped to ask about. */
  awaiting_node?: string | null;
  awaiting_label?: string | null;
  virtual_duration_ms: number;
  variables: ProcessVariables;
  incidents: Array<{ node: string; message: string; hint?: string }>;
  steps: Array<{
    i: number;
    clock: string;
    node: string;
    event: SimulationRun['steps'][number]['event'];
    tokens: string[];
    flow?: string;
    variables_delta?: ProcessVariables;
    note: string;
    decision?: { expression: string; result: string; chose?: string; rule?: string };
    incident?: { node: string; message: string; hint?: string };
  }>;
}

/**
 * Where a snippet should point so it runs unedited.
 *
 * `API_BASE_URL` is relative in every normal deployment, because the server
 * serves this bundle and the API from one origin. A snippet has to be absolute
 * to be runnable, so it is resolved against wherever this page is.
 */
export function simulationBaseUrl(): string {
  if (API_BASE_URL.startsWith('http')) {
    return API_BASE_URL;
  }
  return `${window.location.origin}${API_BASE_URL}`;
}

/** Nothing here reshapes meaning — it only renames fields the wire spells differently. */
function toRun(wire: WireRun): SimulationRun {
  return {
    runId: wire.run_id,
    definition: wire.definition,
    outcome: wire.outcome,
    endedAtNode: wire.ended_at_node,
    awaitingNode: wire.awaiting_node ?? null,
    awaitingLabel: wire.awaiting_label ?? null,
    virtualDurationMs: wire.virtual_duration_ms,
    variables: wire.variables ?? {},
    incidents: wire.incidents ?? [],
    steps: (wire.steps ?? []).map((step) => ({
      i: step.i,
      clock: step.clock,
      node: step.node,
      event: step.event,
      tokens: step.tokens ?? [],
      flow: step.flow,
      variablesDelta: step.variables_delta,
      note: step.note,
      decision: step.decision,
      incident: step.incident,
    })),
  };
}

export async function runSimulation(
  request: SimulationRequest,
  signal?: AbortSignal,
): Promise<SimulationRun> {
  const wire = await requestJSON<WireRun>(SIMULATION_PATH, {
    method: 'POST',
    body: request,
    signal,
  });
  return toRun(wire);
}

/**
 * A whole scenario suite in one call.
 *
 * One request rather than one per scenario because the server holds a
 * transaction open for each run: N parallel browser requests is N concurrent
 * transactions against the pool that real instances are also using.
 */
export async function runSimulationBatch(
  requests: SimulationRequest[],
  signal?: AbortSignal,
): Promise<SimulationRun[]> {
  const wire = await requestJSON<{ runs: WireRun[] }>(`${SIMULATION_PATH}/batch`, {
    method: 'POST',
    body: { scenarios: requests },
    signal,
  });
  return (wire.runs ?? []).map(toRun);
}
