import { describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';

import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';
import {
  decorateEdges,
  decorateNodes,
  describeNodeState,
  nodeStateColor,
  nodeStyleFor,
  type SimulationDecoration,
} from './simulationCanvas';

const nodes = [
  { id: 'Start', position: { x: 0, y: 0 }, data: { label: 'Start' } },
  { id: 'Approve', position: { x: 100, y: 0 }, data: { label: 'Approve' } },
  { id: 'CFO', position: { x: 200, y: 0 }, data: { label: 'CFO signs off' } },
] as Node<BPMNNodeData>[];

const edges = [
  { id: 'Flow_Under', source: 'Gateway', target: 'Approve' },
  { id: 'Flow_Over', source: 'Gateway', target: 'CFO' },
] as Edge<BPMNEdgeData>[];

const decoration: SimulationDecoration = {
  active: true,
  nodeStates: { Start: 'done', Approve: 'active' },
  flowStates: { Flow_Under: 'taken' },
  tokens: ['Approve'],
  awaiting: null,
  answered: [],
};

describe('decorateNodes', () => {
  it('leaves the canvas alone when no run exists', () => {
    expect(decorateNodes(nodes, { ...decoration, active: false })).toBe(nodes);
  });

  it('outlines each reached node in its state colour', () => {
    const [start, approve] = decorateNodes(nodes, decoration);
    expect(start.style?.outline).toContain('teal');
    expect(approve.style?.outline).toContain('blue');
  });

  it('fades a node the run never reached rather than marking it failed', () => {
    const cfo = decorateNodes(nodes, decoration)[2];
    expect(cfo.data.simState).toBe('pending');
    expect(cfo.style?.opacity).toBeLessThan(1);
    expect(cfo.style?.outline).toBeUndefined();
  });

  it('marks the node holding a token', () => {
    const decorated = decorateNodes(nodes, decoration);
    expect(decorated[1].data.simHoldingToken).toBe(true);
    expect(decorated[0].data.simHoldingToken).toBe(false);
  });

  it('locks dragging, so the diagram cannot drift from the run it produced', () => {
    expect(decorateNodes(nodes, decoration).every((node) => node.draggable === false)).toBe(true);
  });

  it('returns new objects, because React Flow compares by reference', () => {
    const decorated = decorateNodes(nodes, decoration);
    expect(decorated[0]).not.toBe(nodes[0]);
    expect(nodes[0].data.simState).toBeUndefined();
  });
});

describe('decorateEdges', () => {
  it('leads with the taken flow and recedes the rest', () => {
    const [taken, untaken] = decorateEdges(edges, decoration);
    expect(taken.style?.opacity).toBe(1);
    expect(taken.animated).toBe(true);
    expect(untaken.style?.opacity).toBeLessThan(0.5);
    expect(untaken.animated).toBe(false);
  });

  it('leaves the canvas alone when no run exists', () => {
    expect(decorateEdges(edges, { ...decoration, active: false })).toBe(edges);
  });
});

describe('the node the run is waiting on', () => {
  const asking: SimulationDecoration = { ...decoration, awaiting: 'CFO' };

  it('gets a halo, so it is findable from anywhere on the canvas', () => {
    const cfo = decorateNodes(nodes, asking)[2];
    expect(cfo.data.simAwaiting).toBe(true);
    expect(cfo.style?.boxShadow).toContain('blue');
    expect(cfo.style?.opacity).toBe(1);
  });

  it('is never faded, even though the run has not reached it', () => {
    expect(nodeStyleFor(undefined, true).opacity).toBe(1);
    expect(nodeStyleFor(undefined, false).opacity).toBeLessThan(1);
  });

  it('outranks the ordinary state treatment', () => {
    expect(nodeStyleFor('done', true).outline).toContain('blue');
  });

  it('marks the steps that already have an answer', () => {
    const decorated = decorateNodes(nodes, { ...decoration, answered: ['Approve'] });
    expect(decorated[1].data.simAnswered).toBe(true);
    expect(decorated[2].data.simAnswered).toBe(false);
  });
});

describe('a node that stopped the run', () => {
  it('gets the same unmissable treatment, in red', () => {
    const style = nodeStyleFor('failed', false);
    expect(style.boxShadow).toContain('red');
    expect(style.outline).toContain('red');
  });
});

describe('describeNodeState', () => {
  it('says what happened in a sentence, for the tooltip and the screen reader alike', () => {
    expect(describeNodeState('Approve', 'active', false)).toBe('Approve is holding a token now');
    expect(describeNodeState('Approve', 'done', false)).toBe('Approve has been through');
    expect(describeNodeState('Approve', 'failed', false)).toBe('Approve stopped the run');
    expect(describeNodeState('Approve', undefined, false)).toBe('Approve was not reached');
  });

  it('leads with the question when the run is waiting on it', () => {
    expect(describeNodeState('Approve', undefined, true)).toBe('Approve is waiting for an answer');
  });
});

describe('nodeStateColor', () => {
  it('gives every state a distinct colour', () => {
    const colors = (['active', 'done', 'failed', 'pending'] as const).map(nodeStateColor);
    expect(new Set(colors).size).toBe(4);
  });
});
