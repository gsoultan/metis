/**
 * "Decide, then take the right path" as one thing you can drop on the canvas.
 *
 * The recommended way to route a process is to let a decision table return an
 * answer and have the gateway conditions be trivial comparisons against it —
 * `= "APPROVED"` — rather than scattering the policy across gateway expressions
 * where it cannot be versioned, tested or read by the person who owns it.
 *
 * Nobody does that by default, because the default is one gateway and two
 * conditions, and that is one drag instead of three. So the recommended shape is
 * made the easy one: a single palette item that lands both pieces, already
 * wired, with the gateway's conditions pointing at what the table decides.
 */

/** Enough of a React Flow node for the builder to make one. */
export interface BuiltNode {
  id: string;
  type: string;
  position: { x: number; y: number };
  data: Record<string, unknown>;
}

export interface BuiltEdge {
  id: string;
  source: string;
  target: string;
  data?: Record<string, unknown>;
  label?: string;
}

export interface BuiltGroup {
  nodes: BuiltNode[];
  edges: BuiltEdge[];
  /** The node to select once it lands — the one the author has to configure. */
  focusId: string;
}

/**
 * The horizontal gap between the two pieces.
 *
 * Wide enough that the connecting edge is visible as an edge rather than as a
 * join, because the whole point is that these are two steps and the author can
 * take them apart.
 */
const GAP = 220;

/**
 * Builds the pair.
 *
 * The gateway's condition is left as a comment rather than guessed at: which
 * output the table returns is not knowable until the author picks a table, and a
 * condition that looks configured but is not is worse than an empty one that
 * says what it wants.
 */
export function buildDecideGroup(
  position: { x: number; y: number },
  makeId: () => string,
  outputVariable = 'decision',
): BuiltGroup {
  const decideId = makeId();
  const routeId = makeId();

  return {
    nodes: [
      {
        id: decideId,
        type: 'businessRuleTask',
        position,
        data: {
          label: 'Decide',
          nodeType: 'businessRuleTask',
          documentation: 'Looks up the answer in a decision table.',
        },
      },
      {
        id: routeId,
        type: 'exclusiveGateway',
        position: { x: position.x + GAP, y: position.y },
        data: {
          label: 'Take the right path',
          nodeType: 'exclusiveGateway',
          documentation: `Branches on what the decision returned. Each path's condition compares ${outputVariable} with one of the table's results.`,
        },
      },
    ],
    edges: [
      {
        id: makeId(),
        source: decideId,
        target: routeId,
      },
    ],
    // The table is what the author must choose; the gateway follows from it.
    focusId: decideId,
  };
}

/** The palette kind that lands the pair. */
export const DECIDE_GROUP_KIND = 'decideGroup';
