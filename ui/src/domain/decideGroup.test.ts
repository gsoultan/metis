import { describe, expect, it } from 'bun:test';

import { buildDecideGroup } from './decideGroup';

function counter() {
  let n = 0;
  return () => `id-${n++}`;
}

/**
 * The recommended way to route a process is to let a decision table return an
 * answer and have the gateway conditions be trivial comparisons against it,
 * rather than scattering policy across gateway expressions where it cannot be
 * versioned, tested or read by the person who owns it.
 *
 * Nobody does that by default, because the default is one drag instead of three.
 */
describe('buildDecideGroup', () => {
  it('lands both pieces, already wired', () => {
    const group = buildDecideGroup({ x: 100, y: 50 }, counter());

    expect(group.nodes.map((node) => node.type)).toEqual(['businessRuleTask', 'exclusiveGateway']);
    expect(group.edges).toHaveLength(1);
    expect(group.edges[0].source).toBe(group.nodes[0].id);
    expect(group.edges[0].target).toBe(group.nodes[1].id);
  });

  it('drops the first piece exactly where it was dropped', () => {
    const group = buildDecideGroup({ x: 100, y: 50 }, counter());
    expect(group.nodes[0].position).toEqual({ x: 100, y: 50 });
  });

  it('leaves a gap wide enough for the edge to read as an edge', () => {
    const group = buildDecideGroup({ x: 0, y: 0 }, counter());
    const gap = group.nodes[1].position.x - group.nodes[0].position.x;
    expect(gap).toBeGreaterThan(150);
    expect(group.nodes[1].position.y).toBe(group.nodes[0].position.y);
  });

  it('selects the table, which is the piece the author has to choose', () => {
    const group = buildDecideGroup({ x: 0, y: 0 }, counter());
    expect(group.focusId).toBe(group.nodes[0].id);
  });

  it('gives every piece its own id', () => {
    const group = buildDecideGroup({ x: 0, y: 0 }, counter());
    const ids = [...group.nodes.map((n) => n.id), ...group.edges.map((e) => e.id)];
    expect(new Set(ids).size).toBe(ids.length);
  });

  /**
   * Which output the table returns is not knowable until the author picks a
   * table. A condition that looks configured but is not is worse than an empty
   * one that says what it wants.
   */
  it('does not invent a condition it cannot know', () => {
    const group = buildDecideGroup({ x: 0, y: 0 }, counter());
    expect(group.edges[0].data).toBeUndefined();
    expect(group.edges[0].label).toBeUndefined();
    // But it does say what the gateway is for.
    expect(String(group.nodes[1].data.documentation)).toContain('decision');
  });

  it('names the variable the author told it about', () => {
    const group = buildDecideGroup({ x: 0, y: 0 }, counter(), 'riskBand');
    expect(String(group.nodes[1].data.documentation)).toContain('riskBand');
  });
});
