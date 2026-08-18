/**
 * How decisions depend on each other.
 *
 * A decision may require others: eligibility feeds risk band, risk band feeds
 * price. The engine already evaluates that graph bottom-up and refuses a cycle —
 * but it refuses it at runtime, in an instance, with an error naming a key. The
 * shape is knowable before then, from the definitions alone.
 *
 * This turns a flat list of decisions into that shape: which depends on which,
 * in what order they would be evaluated, and where a cycle makes them
 * un-evaluable. It is pure so the answers can be checked; the drawing is
 * somebody else's problem.
 */

/** A decision, as much of one as the graph needs. */
export interface GraphDecision {
  id: string;
  key: string;
  name?: string;
  required_decisions?: string[];
}

export interface GraphNode {
  id: string;
  key: string;
  label: string;
  /**
   * How deep in the dependency chain: 0 depends on nothing, 1 depends only on
   * layer 0, and so on. This is evaluation order — layer 0 is decided first.
   */
  layer: number;
  /** True when this decision is part of a cycle, or depends on one. */
  inCycle: boolean;
  /** Keys it names that no decision in this project answers to. */
  missing: string[];
}

export interface GraphEdge {
  /** The decision that is required. */
  from: string;
  /** The decision that requires it. */
  to: string;
  /** True when this edge is part of a cycle. */
  inCycle: boolean;
}

export interface DecisionGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  /** The cycles found, each as the keys around it. */
  cycles: string[][];
}

/**
 * Builds the dependency graph for a project's decisions.
 *
 * Edges point the way the data flows — from the decision that is required to the
 * one that requires it — because that is the order they are evaluated in, and a
 * graph drawn against evaluation order is one nobody can read.
 */
export function buildDecisionGraph(decisions: GraphDecision[]): DecisionGraph {
  const byKey = new Map<string, GraphDecision>();
  for (const decision of decisions) byKey.set(decision.key, decision);

  const cycles = findCycles(decisions, byKey);
  const inCycle = new Set(cycles.flat());

  const layers = computeLayers(decisions, byKey, inCycle);

  const nodes: GraphNode[] = decisions.map((decision) => ({
    id: decision.id,
    key: decision.key,
    label: decision.name || decision.key,
    layer: layers.get(decision.key) ?? 0,
    inCycle: inCycle.has(decision.key),
    missing: (decision.required_decisions ?? []).filter((key) => !byKey.has(key)),
  }));

  const edges: GraphEdge[] = [];
  for (const decision of decisions) {
    for (const required of decision.required_decisions ?? []) {
      if (!byKey.has(required)) continue;
      edges.push({
        from: required,
        to: decision.key,
        inCycle: inCycle.has(required) && inCycle.has(decision.key),
      });
    }
  }

  return { nodes, edges, cycles };
}

/**
 * Depth-first search for cycles.
 *
 * A cycle is not a curiosity: the engine refuses to evaluate one, so every
 * process that reaches any decision in it fails. Finding them here means finding
 * them while looking at the decisions rather than at a stalled instance.
 */
function findCycles(decisions: GraphDecision[], byKey: Map<string, GraphDecision>): string[][] {
  const cycles: string[][] = [];
  const seen = new Set<string>();
  const onPath: string[] = [];
  const onPathSet = new Set<string>();

  const visit = (key: string) => {
    if (onPathSet.has(key)) {
      // Found one: everything from where this key first appears, round to here.
      const start = onPath.indexOf(key);
      cycles.push(onPath.slice(start));
      return;
    }
    if (seen.has(key)) return;

    seen.add(key);
    onPath.push(key);
    onPathSet.add(key);

    for (const required of byKey.get(key)?.required_decisions ?? []) {
      if (byKey.has(required)) visit(required);
    }

    onPath.pop();
    onPathSet.delete(key);
  };

  for (const decision of decisions) visit(decision.key);
  return cycles;
}

/**
 * Assigns each decision a depth: how far it is from depending on nothing.
 *
 * Decisions in a cycle have no meaningful depth — that is what a cycle means —
 * so they are pinned to zero and marked rather than being allowed to loop the
 * calculation forever.
 */
function computeLayers(
  decisions: GraphDecision[],
  byKey: Map<string, GraphDecision>,
  inCycle: Set<string>,
): Map<string, number> {
  const layers = new Map<string, number>();

  const depthOf = (key: string, visiting: Set<string>): number => {
    if (layers.has(key)) return layers.get(key) as number;
    if (inCycle.has(key) || visiting.has(key)) return 0;

    visiting.add(key);
    let deepest = -1;
    for (const required of byKey.get(key)?.required_decisions ?? []) {
      if (byKey.has(required)) {
        deepest = Math.max(deepest, depthOf(required, visiting));
      }
    }
    visiting.delete(key);

    const layer = deepest + 1;
    layers.set(key, layer);
    return layer;
  };

  for (const decision of decisions) depthOf(decision.key, new Set());
  return layers;
}

/** The order the engine would evaluate these in, deepest dependency first. */
export function evaluationOrder(graph: DecisionGraph): string[] {
  return [...graph.nodes]
    .sort((a, b) => a.layer - b.layer || a.key.localeCompare(b.key))
    .map((node) => node.key);
}
