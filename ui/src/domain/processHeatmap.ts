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

/**
 * Where one process's running work is sitting, as the server counts it
 * (`GET /api/v1/projects/{id}/waiting`).
 */
export interface WaitingProcess {
  key: string;
  name: string;
  /** Running instances sitting on at least one step. */
  instances: number;
  steps?: Array<{ node_id: string; waiting: number }>;
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

/**
 * The heat the dashboard draws, from the server's counts.
 *
 * Counted on the server across all of the project's running work. It used to
 * be counted here from the newest 25 instances, reading each one's process from
 * a field the server never sends: every process was "Unknown process", steps
 * with the same id were added together across processes, and at volume the
 * longest-stuck work was the first to fall off the page.
 */
export function heatFromWaiting(processes: readonly WaitingProcess[]): ProcessHeat[] {
  const result: ProcessHeat[] = [];
  for (const process of processes) {
    const steps = (process.steps ?? []).filter((step) => step.node_id && step.waiting > 0);
    if (steps.length === 0) continue;
    const busiest = Math.max(...steps.map((step) => step.waiting));
    const nodes: NodeHeat[] = steps
      .map((step) => ({
        nodeId: step.node_id,
        label: humanizeNodeId(step.node_id),
        waiting: step.waiting,
        intensity: busiest === 0 ? 0 : step.waiting / busiest,
      }))
      .sort((a, b) => b.waiting - a.waiting || a.label.localeCompare(b.label));
    result.push({ processName: process.name || process.key || 'Unknown process', nodes, instances: process.instances });
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
