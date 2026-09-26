import { describe, expect, it } from 'bun:test';

import { busiestStep, heatColor, heatFromWaiting, heatmapCsv, heatSummary, type WaitingProcess } from './processHeatmap';

const waiting = (key: string, name: string, instances: number, steps: Array<[string, number]>): WaitingProcess => ({
  key,
  name,
  instances,
  steps: steps.map(([node_id, count]) => ({ node_id, waiting: count })),
});

describe('heatFromWaiting', () => {
  it('lists each step with how much work is sitting on it, the most first', () => {
    const heat = heatFromWaiting([waiting('quotation', 'Quotation approval', 3, [['salesApprove', 1], ['opsApprove', 2]])]);
    expect(heat).toHaveLength(1);
    expect(heat[0].nodes.map((n) => [n.nodeId, n.waiting])).toEqual([
      ['opsApprove', 2],
      ['salesApprove', 1],
    ]);
  });

  it('scores heat against the busiest step rather than an absolute', () => {
    // Forty waiting is a crisis in a process that normally holds two and a
    // quiet morning in one that holds four hundred, so the scale is relative.
    const heat = heatFromWaiting([waiting('quotation', 'Quotation approval', 4, [['opsApprove', 3], ['salesApprove', 1]])]);
    expect(heat[0].nodes[0].intensity).toBe(1);
    expect(heat[0].nodes[1].intensity).toBeCloseTo(1 / 3);
  });

  it('keeps processes apart, because a step id is only unique within one', () => {
    // The old count read each instance's process from a key the server never
    // sent, and put two processes' "approve" steps together as one.
    const heat = heatFromWaiting([
      waiting('quotation', 'Quotation approval', 1, [['approve', 1]]),
      waiting('onboarding', 'Onboarding', 2, [['approve', 2]]),
    ]);
    expect(heat.map((p) => [p.processName, p.instances])).toEqual([
      ['Onboarding', 2],
      ['Quotation approval', 1],
    ]);
  });

  it('names a process by its key when it has no name', () => {
    expect(heatFromWaiting([waiting('quotation', '', 1, [['approve', 1]])])[0].processName).toBe('quotation');
  });

  it('draws nothing for a process with no step holding work', () => {
    expect(heatFromWaiting([waiting('quotation', 'Quotation approval', 0, [])])).toEqual([]);
    expect(heatFromWaiting([])).toEqual([]);
  });

  it('reads a step id the way a person would', () => {
    const heat = heatFromWaiting([waiting('quotation', 'Quotation approval', 1, [['Activity_AskDirector', 1]])]);
    expect(heat[0].nodes[0].label).toBe('Ask Director');
  });
});

describe('busiestStep', () => {
  it('finds the worst step across every process', () => {
    const heat = heatFromWaiting([
      waiting('quotation', 'Quotation approval', 1, [['opsApprove', 1]]),
      waiting('onboarding', 'Onboarding', 2, [['legalReview', 2]]),
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
    const heat = heatFromWaiting([waiting('quotation', 'Quotation approval', 2, [['opsApprove', 2]])]);
    expect(heatSummary(heat)).toBe('2 instances are waiting on "Ops Approve" in Quotation approval.');
  });

  it('reads one as one', () => {
    expect(heatSummary(heatFromWaiting([waiting('quotation', 'Quotation approval', 1, [['opsApprove', 1]])]))).toBe(
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

describe('heatmapCsv', () => {
  it('writes one row per step, as the dashboard lists them', () => {
    const heat = heatFromWaiting([
      { key: 'quotation', name: 'Quotation approval', instances: 5, steps: [{ node_id: 'managerReview', waiting: 3 }, { node_id: 'finance', waiting: 2 }] },
    ]);
    expect(heatmapCsv(heat)).toBe(
      'Process,Step,Waiting\r\nQuotation approval,Manager Review,3\r\nQuotation approval,Finance,2',
    );
  });

  it('keeps a process name a spreadsheet would run as a formula as text', () => {
    const heat = heatFromWaiting([{ key: 'x', name: '=HYPERLINK("http://evil")', instances: 1, steps: [{ node_id: 'a', waiting: 1 }] }]);
    expect(heatmapCsv(heat).split('\r\n')[1].startsWith(`"'=HYPERLINK`)).toBe(true);
  });
});
