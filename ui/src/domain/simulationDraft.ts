/**
 * The canvas as a draft the simulator can run.
 *
 * Before this, Simulate only ran the deployed version, so trying a change meant
 * deploying it — and deploying makes it the version new instances start on.
 * Now the designer sends what is on the canvas and the server runs it inside
 * the same rolled-back transaction as every other simulation, checked the way a
 * deploy is checked and never saved.
 *
 * The draft goes out in the shape `POST /api/v1/definitions` reads, because the
 * simulation endpoint decodes it into the same entity. The designer's payload
 * names people by username and groups by name; the entity holds them as
 * objects, so those two fields are the only ones reshaped here.
 */
import type { CreateDefinitionPayload, CreateFlowPayload, CreateNodePayload } from '../services/types';

export interface DraftNode extends Omit<CreateNodePayload, 'candidate_users' | 'candidate_groups'> {
  candidate_users: { username: string }[];
  candidate_groups: { name: string }[];
}

export interface DraftDefinition {
  key: string;
  name: string;
  nodes: DraftNode[];
  flows: CreateFlowPayload[];
}

export function toDraftDefinition(payload: CreateDefinitionPayload): DraftDefinition {
  return {
    key: payload.key,
    name: payload.name,
    nodes: payload.nodes.map((node) => ({
      ...node,
      // The server reads these as integers and refuses the whole request on a
      // fraction. Positions are already rounded by the mapper; sizes come from
      // whatever the node was given, so they are rounded here.
      width: Math.round(node.width),
      height: Math.round(node.height),
      candidate_users: node.candidate_users.map((username) => ({ username })),
      candidate_groups: node.candidate_groups.map((name) => ({ name })),
    })),
    flows: payload.flows,
  };
}

/**
 * What the process *does*, with where its shapes sit left out.
 *
 * Two canvases with the same fingerprint run the same way, so moving a box does
 * not turn "the deployed version" into "a draft" — which would cost the case
 * its CI snippet for no change in behaviour. Keys are sorted, so two payloads
 * built in a different property order still compare equal.
 */
export function definitionFingerprint(payload: CreateDefinitionPayload): string {
  const nodes = payload.nodes.map((node) => {
    const { x: _x, y: _y, width: _width, height: _height, is_expanded: _expanded, ...behaviour } = node;
    return behaviour;
  });
  return sortedJSON({ ...payload, nodes });
}

/**
 * What the process does *and where its shapes sit*: what a person would lose
 * if the tab closed. Sizes are left out, because the canvas measures them
 * after loading and an untouched process would otherwise read as changed.
 */
export function layoutFingerprint(payload: CreateDefinitionPayload): string {
  const nodes = payload.nodes.map((node) => {
    const { width: _width, height: _height, is_expanded: _expanded, ...placed } = node;
    return placed;
  });
  return sortedJSON({ ...payload, nodes });
}

/** JSON with every object's keys sorted, so property order does not matter. */
function sortedJSON(value: unknown): string {
  return JSON.stringify(value, (_field: string, inner: unknown) => {
    if (inner === null || typeof inner !== 'object' || Array.isArray(inner)) return inner;
    return Object.fromEntries(Object.entries(inner).sort(([a], [b]) => a.localeCompare(b)));
  });
}
