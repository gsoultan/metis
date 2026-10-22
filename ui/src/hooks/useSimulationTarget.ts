/**
 * What Simulate runs: the canvas as it stands.
 *
 * When the canvas still does exactly what the loaded version does, that is the
 * version that runs — pinned by number, so the case's CI snippet names the
 * version the person is looking at rather than whichever one happens to be
 * live. Once the canvas does anything else, or has never been deployed, it runs
 * as a draft: checked the way a deploy is checked, run on the real engine, and
 * never saved. Testing a change no longer means deploying it.
 *
 * Only built while simulating. Turning the canvas into a payload walks every
 * node, and nobody needs that on every drag in design mode.
 */
import type { Edge, Node } from '@xyflow/react';
import { useMemo } from 'react';

import { definitionFingerprint, toDraftDefinition } from '../domain/simulationDraft';
import type { SimulationTarget } from '../domain/simulationScenario';
import { buildDefinitionPayload } from '../mappers/definitionMapper';
import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';

export interface DeployedVersion {
  version: number;
  fingerprint: string;
}

export function useSimulationTarget({
  active,
  projectId,
  deployed,
  processName,
  processKey,
  nodes,
  edges,
}: {
  active: boolean;
  projectId: string | null;
  deployed: DeployedVersion | null;
  processName: string;
  processKey: string;
  nodes: Node<BPMNNodeData>[];
  edges: Edge<BPMNEdgeData>[];
}): SimulationTarget | null {
  return useMemo(() => {
    if (!active || projectId === null || processKey.trim() === '') return null;

    const payload = buildDefinitionPayload(processName, processKey, nodes, edges);
    if (deployed !== null && definitionFingerprint(payload) === deployed.fingerprint) {
      return { kind: 'deployed', projectId, definitionKey: processKey, version: deployed.version };
    }
    return { kind: 'draft', projectId, definitionKey: processKey, definition: toDraftDefinition(payload) };
  }, [active, deployed, edges, nodes, processKey, processName, projectId]);
}
