import { humanizeNodeId } from './instanceList';

/**
 * Where work is piling up.
 *
 * Every instance list already says which step each instance is sitting on. One
 * row at a time that answers "where is this quotation"; nobody could ask the
 * question the other way round — which step is holding the most work — which is
 * the one that finds a bottleneck.
 *
 * It is deliberately about *now* rather than about history. A count of how many
 * times a node has ever run is a different report and a flattering one: a step
 * that ran ten thousand times without ever delaying anything looks busiest, and
 * the step where forty quotations are stuck right now looks quiet.
 */

/** An instance, as the heatmap needs it. */
export interface HeatmapInstance {
  id: string;
  status?: string;
  definition?: { id?: string; key?: string; name?: string };
  /** The steps this instance is sitting on. Usually one; more with parallelism. */
  activeNodes?: Array<{ id: string }>;
}

/** One step, and how much work is waiting on it. */
export interface NodeHeat {
  nodeId: string;
  /** The step's name as a person would read it. */
  label: string;
  /** How many instances are sitting on it right now. */
  waiting: number;
  /**
   * How hot, from 0 to 1, relative to the busiest step.
   *
   * Relative rather than absolute because there is no absolute: forty waiting
   * is a crisis in a process that normally holds two and a quiet morning in one
   * that holds four hundred.
   */
  intensity: number;
}

export interface ProcessHeat {
  /** The process these steps belong to, as a person would name it. */
  processName: string;
  nodes: NodeHeat[];
  /** Instances counted — running ones sitting on a step. */
  instances: number;
}

/** Statuses that mean the instance is still somewhere. */
function isLive(status?: string): boolean {
  const value = (status ?? '').toLowerCase();
  return value === '' || value === 'active' || value === 'suspended';
}

/**
 * Counts where the running work is, one entry per process.
 *
 * Grouped by process because a step id is only unique within one: two processes
 * can both have "approve", and adding them together produces a number that is
 * about nothing.
 */
export function processHeat(instances: readonly HeatmapInstance[]): ProcessHeat[] {
  const byProcess = new Map<string, { name: string; counts: Map<string, number>; instances: number }>();

  for (const instance of instances) {
    if (!isLive(instance.status)) continue;
    const nodes = instance.activeNodes ?? [];
    if (nodes.length === 0) continue;

    const key = instance.definition?.key ?? instance.definition?.id ?? 'unknown';
    const name = instance.definition?.name || instance.definition?.key || 'Unknown process';
    let group = byProcess.get(key);
    if (!group) {
      group = { name, counts: new Map(), instances: 0 };
      byProcess.set(key, group);
    }
    group.instances += 1;

    // An instance on two branches is waiting in two places, and both places are
    // holding it up. Counting it once would hide the branch that is stuck.
    for (const node of nodes) {
      if (!node?.id) continue;
      group.counts.set(node.id, (group.counts.get(node.id) ?? 0) + 1);
    }
  }

  const result: ProcessHeat[] = [];
  for (const group of byProcess.values()) {
    const busiest = Math.max(...group.counts.values(), 0);
    const nodes: NodeHeat[] = [...group.counts.entries()]
      .map(([nodeId, waiting]) => ({
        nodeId,
        label: humanizeNodeId(nodeId),
        waiting,
        intensity: busiest === 0 ? 0 : waiting / busiest,
      }))
      .sort((a, b) => b.waiting - a.waiting || a.label.localeCompare(b.label));
    result.push({ processName: group.name, nodes, instances: group.instances });
  }

  // The process holding the most work first: that is where somebody looks.
  return result.sort(
    (a, b) => b.instances - a.instances || a.processName.localeCompare(b.processName),
  );
}

/**
 * The single step holding the most work, across every process.
 *
 * Returns null when nothing is waiting anywhere, which is a real answer and not
 * an empty list to render.
 */
export function busiestStep(heat: readonly ProcessHeat[]): (NodeHeat & { processName: string }) | null {
  let worst: (NodeHeat & { processName: string }) | null = null;
  for (const process of heat) {
    for (const node of process.nodes) {
      if (!worst || node.waiting > worst.waiting) {
        worst = { ...node, processName: process.processName };
      }
    }
  }
  return worst;
}

/**
 * One sentence about where the work is.
 *
 * Named rather than counted: "12 waiting" is a number somebody has to go and
 * interpret, and "12 are waiting on Operations approve" is the thing they were
 * going to find out.
 */
export function heatSummary(heat: readonly ProcessHeat[]): string {
  const worst = busiestStep(heat);
  if (!worst) return 'No running work is waiting on a step.';
  if (worst.waiting === 1) {
    return `One instance is waiting on "${worst.label}" in ${worst.processName}.`;
  }
  return `${worst.waiting} instances are waiting on "${worst.label}" in ${worst.processName}.`;
}

/**
 * A Mantine colour for a step's heat.
 *
 * Three bands rather than a gradient: the question is "is this the problem, is
 * it near it, or is it fine", and a continuous scale invites reading a
 * difference between 0.61 and 0.68 that means nothing.
 */
export function heatColor(intensity: number): string {
  if (intensity >= 0.66) return 'red';
  if (intensity >= 0.33) return 'orange';
  return 'blue';
}
