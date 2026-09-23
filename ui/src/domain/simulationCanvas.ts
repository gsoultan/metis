/**
 * Dressing the canvas in a simulation's state.
 *
 * The diagram is the output. Everything else on the screen exists to help read
 * it, so this file is careful about how loud it is: one accent for where the
 * run is, one for where it has been, and everything untouched left alone rather
 * than greyed into noise.
 *
 * Two things are deliberately unmissable. The node the run is *waiting on*, and
 * the node that *stopped* it — those are the two moments somebody is looking
 * for, and they get a ring and a halo rather than a colour change that has to
 * compete with whatever the node already looks like.
 */
import type { Edge, Node } from '@xyflow/react';

import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';
import type { FlowSimState, NodeSimState } from './simulationTrace';

/** How far an untaken branch recedes. Low enough to read as "not this way". */
const UNTAKEN_OPACITY = 0.18;

/** A node the run has not reached. Quiet, but still legible. */
const UNREACHED_OPACITY = 0.5;

export interface SimulationDecoration {
  nodeStates: Record<string, NodeSimState>;
  flowStates: Record<string, FlowSimState>;
  /** Node ids holding a token right now. */
  tokens: string[];
  /** The node whose answer the run is waiting for, if any. */
  awaiting: string | null;
  /** Nodes that already have an answer, so the diagram shows what is set up. */
  answered: string[];
  /** True once a run exists; before that the canvas is drawn normally. */
  active: boolean;
}

export function nodeStateColor(state: NodeSimState): string {
  switch (state) {
    case 'active':
      return 'var(--mantine-color-blue-5)';
    case 'done':
      return 'var(--mantine-color-teal-5)';
    case 'failed':
      return 'var(--mantine-color-red-6)';
    case 'pending':
      return 'var(--mantine-color-gray-4)';
  }
}

/**
 * What to say about a node, for a screen reader and for the tooltip alike.
 *
 * A sentence rather than a status word: "Approve: active" tells somebody using
 * a screen reader less than "Approve is holding a token now" does, and both
 * audiences want the same thing.
 */
export function describeNodeState(
  label: string,
  state: NodeSimState | undefined,
  awaiting: boolean,
): string {
  if (awaiting) return `${label} is waiting for an answer`;
  switch (state) {
    case 'active':
      return `${label} is holding a token now`;
    case 'done':
      return `${label} has been through`;
    case 'failed':
      return `${label} stopped the run`;
    default:
      return `${label} was not reached`;
  }
}

/**
 * The visual treatment for one node, as plain values.
 *
 * Split out from `decorateNodes` so the rule is testable without building a
 * React Flow node around it, and so there is exactly one place that decides
 * how loud any given state is.
 */
export function nodeStyleFor(
  state: NodeSimState | undefined,
  awaiting: boolean,
): { opacity: number; boxShadow?: string; outline?: string } {
  if (awaiting) {
    // The question the run is asking. A halo, so it reads at a glance from
    // anywhere on the canvas without the node itself changing colour.
    return {
      opacity: 1,
      outline: '2px solid var(--mantine-color-blue-6)',
      boxShadow: '0 0 0 6px color-mix(in srgb, var(--mantine-color-blue-5) 22%, transparent)',
    };
  }

  if (state === 'failed') {
    return {
      opacity: 1,
      outline: '2px solid var(--mantine-color-red-6)',
      boxShadow: '0 0 0 6px color-mix(in srgb, var(--mantine-color-red-5) 20%, transparent)',
    };
  }

  if (state === undefined) {
    return { opacity: UNREACHED_OPACITY };
  }

  return { opacity: 1, outline: `2px solid ${nodeStateColor(state)}` };
}

/**
 * Apply a run's state to the nodes the designer is already drawing.
 *
 * Returns a new array; React Flow compares by reference and mutating in place
 * would leave the canvas showing the previous step.
 */
export function decorateNodes(
  nodes: Node<BPMNNodeData>[],
  decoration: SimulationDecoration,
): Node<BPMNNodeData>[] {
  if (!decoration.active) return nodes;

  const holding = new Set(decoration.tokens);
  const answered = new Set(decoration.answered);

  return nodes.map((node) => {
    const state = decoration.nodeStates[node.id];
    const awaiting = decoration.awaiting === node.id;
    const style = nodeStyleFor(state, awaiting);

    return {
      ...node,
      // Dragging during a run would edit the model the run was made from, and
      // the canvas would then be showing a trace of something else.
      draggable: false,
      data: {
        ...node.data,
        simState: state ?? 'pending',
        simHoldingToken: holding.has(node.id),
        simAwaiting: awaiting,
        simAnswered: answered.has(node.id),
        simLabel: describeNodeState(asLabel(node.data), state, awaiting),
      },
      style: {
        ...node.style,
        ...style,
        outlineOffset: 3,
        borderRadius: 'var(--mantine-radius-sm)',
        transition: 'opacity 160ms ease, box-shadow 160ms ease',
      },
    };
  });
}

/** Apply a run's state to the edges: taken flows lead, untaken ones recede. */
export function decorateEdges(
  edges: Edge<BPMNEdgeData>[],
  decoration: SimulationDecoration,
): Edge<BPMNEdgeData>[] {
  if (!decoration.active) return edges;

  return edges.map((edge) => {
    const taken = decoration.flowStates[edge.id] === 'taken';
    return {
      ...edge,
      // Only the path the run took animates. Animating everything would make an
      // untaken branch look like it was carrying something.
      animated: taken,
      style: {
        ...edge.style,
        opacity: taken ? 1 : UNTAKEN_OPACITY,
        strokeWidth: taken ? 2.5 : 1.5,
        stroke: taken ? 'var(--mantine-color-teal-5)' : undefined,
        transition: 'opacity 160ms ease',
      },
    };
  });
}

/** Node labels are typed loosely in React Flow's data bag; read one safely. */
function asLabel(data: BPMNNodeData): string {
  return typeof data.label === 'string' && data.label !== '' ? data.label : 'This step';
}
