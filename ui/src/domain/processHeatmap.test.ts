import { describe, expect, it } from 'bun:test';

import {
  busiestStep,
  heatColor,
  heatSummary,
  processHeat,
  type HeatmapInstance,
} from './processHeatmap';

const quotation = { key: 'quotation', name: 'Quotation approval' };
const onboarding = { key: 'onboarding', name: 'Onboarding' };

const at = (id: string, node: string, over: Partial<HeatmapInstance> = {}): HeatmapInstance => ({
  id,
  status: 'active',
  definition: quotation,
  activeNodes: [{ id: node }],
  ...over,
});

describe('processHeat', () => {
  it('counts how many instances are sitting on each step', () => {
    const heat = processHeat([
      at('1', 'opsApprove'),
      at('2', 'opsApprove'),
      at('3', 'salesApprove'),
    ]);

    expect(heat).toHaveLength(1);
    expect(heat[0].nodes.map((n) => [n.nodeId, n.waiting])).toEqual([
      ['opsApprove', 2],
      ['salesApprove', 1],
    ]);
  });

  it('scores heat against the busiest step rather than an absolute', () => {
    // Forty waiting is a crisis in a process that normally holds two and a
    // quiet morning in one that holds four hundred, so the scale is relative.
    const heat = processHeat([
      at('1', 'opsApprove'),
      at('2', 'opsApprove'),
      at('3', 'opsApprove'),
      at('4', 'salesApprove'),
    ]);
    expect(heat[0].nodes[0].intensity).toBe(1);
    expect(heat[0].nodes[1].intensity).toBeCloseTo(1 / 3);
  });

  it('keeps processes apart, because a step id is only unique within one', () => {
    const heat = processHeat([
      at('1', 'approve'),
      at('2', 'approve', { definition: onboarding }),
      at('3', 'approve', { definition: onboarding }),
    ]);

    // Adding the two "approve" steps together would produce a number about
    // nothing. Busiest process first.
    expect(heat.map((p) => [p.processName, p.instances])).toEqual([
      ['Onboarding', 2],
      ['Quotation approval', 1],
    ]);
  });

  it('counts an instance in every place it is waiting', () => {
    // Parallel branches: it is held up in two places and both are holding it.
    const heat = processHeat([
      at('1', 'x', { activeNodes: [{ id: 'legalReview' }, { id: 'creditCheck' }] }),
    ]);
    expect(heat[0].nodes.map((n) => n.waiting)).toEqual([1, 1]);
    expect(heat[0].instances).toBe(1);
  });

  it('ignores work that has stopped', () => {
    const heat = processHeat([
      at('1', 'opsApprove', { status: 'completed' }),
      at('2', 'opsApprove', { status: 'cancelled' }),
      at('3', 'opsApprove', { status: 'failed' }),
      at('4', 'opsApprove', { status: 'suspended' }),
    ]);
    // Suspended work is still somewhere — it is paused, not finished.
    expect(heat[0].nodes[0].waiting).toBe(1);
  });

  it('ignores an instance that is not sitting anywhere yet', () => {
    expect(processHeat([at('1', 'x', { activeNodes: [] })])).toEqual([]);
    expect(processHeat([])).toEqual([]);
  });

  it('reads a step id the way a person would', () => {
    const heat = processHeat([at('1', 'Activity_AskDirector')]);
    expect(heat[0].nodes[0].label).toBe('Ask Director');
  });
});

describe('busiestStep', () => {
  it('finds the worst step across every process', () => {
    const heat = processHeat([
      at('1', 'opsApprove'),
      at('2', 'legalReview', { definition: onboarding }),
      at('3', 'legalReview', { definition: onboarding }),
    ]);
    expect(busiestStep(heat)).toMatchObject({
      nodeId: 'legalReview',
      waiting: 2,
      processName: 'Onboarding',
    });
  });

  it('says nothing rather than an empty list when nothing is waiting', () => {
    expect(busiestStep([])).toBeNull();
  });
});

describe('heatSummary', () => {
  it('names the step rather than quoting a number to interpret', () => {
    const heat = processHeat([at('1', 'opsApprove'), at('2', 'opsApprove')]);
    expect(heatSummary(heat)).toBe('2 instances are waiting on "Ops Approve" in Quotation approval.');
  });

  it('reads one as one', () => {
    expect(heatSummary(processHeat([at('1', 'opsApprove')]))).toBe(
      'One instance is waiting on "Ops Approve" in Quotation approval.',
    );
  });

  it('says the quiet answer plainly', () => {
    expect(heatSummary([])).toBe('No running work is waiting on a step.');
  });
});

describe('heatColor', () => {
  it('bands rather than gradients, because 0.61 and 0.68 do not differ', () => {
    expect(heatColor(1)).toBe('red');
    expect(heatColor(0.7)).toBe('red');
    expect(heatColor(0.5)).toBe('orange');
    expect(heatColor(0.2)).toBe('blue');
    expect(heatColor(0)).toBe('blue');
  });
});
