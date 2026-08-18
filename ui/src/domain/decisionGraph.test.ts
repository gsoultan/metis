import { describe, expect, it } from 'bun:test';

import { buildDecisionGraph, evaluationOrder, type GraphDecision } from './decisionGraph';

function decision(key: string, requires: string[] = []): GraphDecision {
  return { id: `id-${key}`, key, name: key, required_decisions: requires };
}

/**
 * Layered policy is the reason RequiredDecisions exists: eligibility feeds risk
 * band, risk band feeds price. The layer is the order the engine evaluates them
 * in, and seeing it is the difference between a flat list and an explanation.
 */
describe('buildDecisionGraph', () => {
  it('puts a chain in evaluation order', () => {
    const graph = buildDecisionGraph([
      decision('price', ['risk']),
      decision('risk', ['eligibility']),
      decision('eligibility'),
    ]);

    const layerOf = Object.fromEntries(graph.nodes.map((node) => [node.key, node.layer]));
    expect(layerOf.eligibility).toBe(0);
    expect(layerOf.risk).toBe(1);
    expect(layerOf.price).toBe(2);
    expect(evaluationOrder(graph)).toEqual(['eligibility', 'risk', 'price']);
  });

  it('points edges the way the data flows, not the way the dependency is written', () => {
    const graph = buildDecisionGraph([decision('price', ['risk']), decision('risk')]);
    expect(graph.edges).toEqual([{ from: 'risk', to: 'price', inCycle: false }]);
  });

  it('gives a decision that depends on nothing the first layer', () => {
    const graph = buildDecisionGraph([decision('a'), decision('b')]);
    expect(graph.nodes.every((node) => node.layer === 0)).toBe(true);
    expect(graph.edges).toEqual([]);
  });

  it('takes the deepest path when a decision has several dependencies', () => {
    // price depends on risk (deep) and on vat (shallow); it must sit past risk.
    const graph = buildDecisionGraph([
      decision('price', ['risk', 'vat']),
      decision('risk', ['eligibility']),
      decision('eligibility'),
      decision('vat'),
    ]);
    const layerOf = Object.fromEntries(graph.nodes.map((node) => [node.key, node.layer]));
    expect(layerOf.price).toBe(2);
    expect(layerOf.vat).toBe(0);
  });
});

/**
 * A cycle is not a curiosity. The engine refuses to evaluate one, so every
 * process reaching any decision in it fails — and it fails at runtime, in an
 * instance, naming a key. Finding it here means finding it while looking at the
 * decisions instead of at a stalled process.
 */
describe('cycles', () => {
  it('finds a direct one', () => {
    const graph = buildDecisionGraph([decision('a', ['b']), decision('b', ['a'])]);

    expect(graph.cycles).toHaveLength(1);
    expect(graph.cycles[0].sort()).toEqual(['a', 'b']);
    expect(graph.nodes.every((node) => node.inCycle)).toBe(true);
    expect(graph.edges.every((edge) => edge.inCycle)).toBe(true);
  });

  it('finds a longer one', () => {
    const graph = buildDecisionGraph([decision('a', ['b']), decision('b', ['c']), decision('c', ['a'])]);
    expect(graph.cycles).toHaveLength(1);
    expect(graph.cycles[0].sort()).toEqual(['a', 'b', 'c']);
  });

  it('finds a decision that requires itself', () => {
    const graph = buildDecisionGraph([decision('a', ['a'])]);
    expect(graph.cycles).toHaveLength(1);
    expect(graph.nodes[0].inCycle).toBe(true);
  });

  it('does not mistake a diamond for a cycle', () => {
    // Two paths to the same dependency is normal, and common.
    const graph = buildDecisionGraph([
      decision('top', ['left', 'right']),
      decision('left', ['bottom']),
      decision('right', ['bottom']),
      decision('bottom'),
    ]);
    expect(graph.cycles).toEqual([]);
    expect(graph.nodes.some((node) => node.inCycle)).toBe(false);
  });

  it('terminates on a cycle rather than looping the layer calculation', () => {
    const graph = buildDecisionGraph([decision('a', ['b']), decision('b', ['a']), decision('c', ['a'])]);
    expect(graph.nodes).toHaveLength(3);
  });
});

/**
 * A decision naming a key nothing answers to is a table that will fail the first
 * time a process reaches it, with an error about a decision nobody can find.
 */
describe('missing dependencies', () => {
  it('names the keys nothing answers to', () => {
    const graph = buildDecisionGraph([decision('price', ['risk', 'deleted-long-ago'])]);
    const price = graph.nodes.find((node) => node.key === 'price');
    expect(price?.missing).toEqual(['risk', 'deleted-long-ago']);
  });

  it('does not draw an edge to something that is not there', () => {
    const graph = buildDecisionGraph([decision('price', ['nowhere'])]);
    expect(graph.edges).toEqual([]);
  });
});
