import { describe, expect, it } from 'bun:test';

import {
  changedKeysAt,
  clampIndex,
  describeClock,
  describeElapsed,
  describeOutcome,
  elapsedMsAt,
  firstDivergence,
  flowStatesAt,
  lastIndex,
  nodeStatesAt,
  passed,
  reached,
  variablesAt,
  verdict,
  type SimulationRun,
  type SimulationStep,
} from './simulationTrace';

const DAY = 86_400_000;

function step(partial: Partial<SimulationStep> & Pick<SimulationStep, 'i' | 'node' | 'event'>): SimulationStep {
  return {
    clock: new Date(Date.UTC(2026, 8, 22, 9, 0) + partial.i * 60_000).toISOString(),
    tokens: [partial.node],
    note: `step ${partial.i}`,
    ...partial,
  };
}

function run(steps: SimulationStep[], overrides: Partial<SimulationRun> = {}): SimulationRun {
  return {
    runId: 'run-1',
    definition: { key: 'expense-approval', version: 4, id: 'def-1' },
    outcome: 'completed',
    endedAtNode: 'End',
    awaitingNode: null,
    awaitingLabel: null,
    virtualDurationMs: 0,
    variables: {},
    incidents: [],
    steps,
    ...overrides,
  };
}

describe('clampIndex', () => {
  it('keeps a cursor inside the trace rather than throwing', () => {
    const r = run([step({ i: 0, node: 'Start', event: 'entered' }), step({ i: 1, node: 'A', event: 'entered' })]);
    expect(clampIndex(r, -5)).toBe(0);
    expect(clampIndex(r, 99)).toBe(1);
    expect(clampIndex(r, 1)).toBe(1);
  });

  it('sits at zero for a run with no steps', () => {
    expect(clampIndex(run([]), 3)).toBe(0);
    expect(lastIndex(run([]))).toBe(-1);
  });
});

describe('nodeStatesAt', () => {
  const trace = run([
    step({ i: 0, node: 'Start', event: 'entered' }),
    step({ i: 1, node: 'Start', event: 'left', tokens: ['Submit'] }),
    step({ i: 2, node: 'Submit', event: 'entered', tokens: ['Submit'] }),
    step({ i: 3, node: 'Submit', event: 'left', tokens: ['Approve'] }),
    step({ i: 4, node: 'Approve', event: 'waiting', tokens: ['Approve'] }),
  ]);

  it('marks a node holding a token active and a node left behind done', () => {
    const states = nodeStatesAt(trace, 4);
    expect(states.Start).toBe('done');
    expect(states.Submit).toBe('done');
    expect(states.Approve).toBe('active');
  });

  it('leaves nodes the run has not reached out of the map entirely', () => {
    expect(nodeStatesAt(trace, 1).Approve).toBeUndefined();
  });

  it('gives the same answer scrubbing back as arriving forwards', () => {
    const arriving = nodeStatesAt(trace, 2);
    const scrubbed = nodeStatesAt(trace, 2);
    expect(scrubbed).toEqual(arriving);
    expect(arriving.Submit).toBe('active');
  });

  it('keeps a failed node failed — the run stopped there', () => {
    const withIncident = run([
      step({ i: 0, node: 'Start', event: 'entered' }),
      step({ i: 1, node: 'Gateway', event: 'incident', tokens: [] }),
    ]);
    expect(nodeStatesAt(withIncident, 1).Gateway).toBe('failed');
  });

  it('re-activates a node a loop returns to', () => {
    const looping = run([
      step({ i: 0, node: 'Review', event: 'entered', tokens: ['Review'] }),
      step({ i: 1, node: 'Review', event: 'left', tokens: ['Fix'] }),
      step({ i: 2, node: 'Fix', event: 'left', tokens: ['Review'] }),
    ]);
    expect(nodeStatesAt(looping, 2).Review).toBe('active');
  });
});

describe('flowStatesAt', () => {
  it('lists only the flows a token actually crossed', () => {
    const trace = run([
      step({ i: 0, node: 'Gateway', event: 'decided' }),
      step({ i: 1, node: 'TeamLead', event: 'entered', flow: 'Flow_Under5000' }),
    ]);
    const states = flowStatesAt(trace, 1);
    expect(states.Flow_Under5000).toBe('taken');
    expect(states.Flow_Over5000).toBeUndefined();
  });
});

describe('variablesAt', () => {
  const trace = run([
    step({ i: 0, node: 'Start', event: 'entered', variablesDelta: { amount: 4200 } }),
    step({ i: 1, node: 'Decide', event: 'decided', variablesDelta: { band: 'standard' } }),
    step({ i: 2, node: 'Approve', event: 'entered', variablesDelta: { band: 'reviewed', approver: 'sarah' } }),
  ]);

  it('accumulates the deltas up to the step asked for', () => {
    expect(variablesAt(trace, 1)).toEqual({ amount: 4200, band: 'standard' });
  });

  it('lets a later step overwrite an earlier value', () => {
    expect(variablesAt(trace, 2)).toEqual({ amount: 4200, band: 'reviewed', approver: 'sarah' });
  });

  it('reports what a single step changed', () => {
    expect(changedKeysAt(trace, 2).sort()).toEqual(['approver', 'band']);
    expect(changedKeysAt(trace, 0)).toEqual(['amount']);
  });
});

describe('the virtual clock', () => {
  it('reads elapsed time in days and hours, not milliseconds', () => {
    expect(describeElapsed(0)).toBe('0m');
    expect(describeElapsed(14 * 60_000)).toBe('14m');
    expect(describeElapsed(DAY + 14 * 60_000)).toBe('1d 0h 14m');
    expect(describeElapsed(2 * 3_600_000 + 5 * 60_000)).toBe('2h 5m');
  });

  it('counts the day a process started as day 1', () => {
    const trace = run([
      step({ i: 0, node: 'Start', event: 'entered' }),
      { ...step({ i: 1, node: 'Approve', event: 'waiting' }), clock: new Date(Date.UTC(2026, 8, 23, 9, 14)).toISOString() },
    ]);
    expect(describeClock(trace, 0)).toBe('Day 1, 09:00');
    expect(describeClock(trace, 1)).toBe('Day 2, 09:14 (+1d 0h 14m)');
    expect(elapsedMsAt(trace, 1)).toBe(DAY + 14 * 60_000);
  });

  it('says so plainly when there is no run', () => {
    expect(describeClock(run([]), 0)).toBe('No run yet');
  });
});

describe('firstDivergence', () => {
  const v3 = run([
    step({ i: 0, node: 'Start', event: 'entered' }),
    step({ i: 1, node: 'Gateway', event: 'decided' }),
    step({ i: 2, node: 'TeamLead', event: 'entered' }),
  ]);

  it('is null when both versions took the same path — the answer that lets you deploy', () => {
    expect(firstDivergence(v3, v3)).toBeNull();
  });

  it('names the step and both nodes where the versions part', () => {
    const v4 = run([
      step({ i: 0, node: 'Start', event: 'entered' }),
      step({ i: 1, node: 'Gateway', event: 'decided' }),
      step({ i: 2, node: 'CFO', event: 'entered' }),
    ]);
    expect(firstDivergence(v3, v4)).toEqual({ index: 2, aNode: 'TeamLead', bNode: 'CFO' });
  });

  it('treats one run stopping early as the divergence', () => {
    const short = run([step({ i: 0, node: 'Start', event: 'entered' })]);
    expect(firstDivergence(short, v3)).toEqual({ index: 1, aNode: '—', bNode: 'Gateway' });
  });
});

describe('outcome reporting', () => {
  it('passes only when the run completed and raised nothing', () => {
    expect(passed(run([]))).toBe(true);
    expect(passed(run([], { incidents: [{ node: 'Gateway', message: 'nothing matched' }] }))).toBe(false);
    expect(passed(run([], { outcome: 'step_budget' }))).toBe(false);
  });

  it('leads with the incident, because that is what tells somebody what to fix', () => {
    const stuck = run([], {
      outcome: 'incident',
      incidents: [{ node: 'Gateway', message: 'Nothing matched at "Over £5,000?"' }],
    });
    expect(describeOutcome(stuck)).toBe('Nothing matched at "Over £5,000?"');
  });

  it('explains a step budget as the loop it usually is', () => {
    expect(verdict(run([], { outcome: 'step_budget' })).detail).toContain('loop with no way out');
  });

  it('reports which nodes a run reached', () => {
    const trace = run([step({ i: 0, node: 'TeamLead', event: 'entered' })]);
    expect(reached(trace, 'TeamLead')).toBe(true);
    expect(reached(trace, 'CFO')).toBe(false);
  });
});

describe('verdict', () => {
  it('treats a run waiting for an answer as the process asking, not as a failure', () => {
    const asking = run([], {
      outcome: 'needs_answer',
      awaitingNode: 'Approve',
      awaitingLabel: 'Approve the expense',
    });
    const v = verdict(asking);
    expect(v.tone).toBe('asking');
    expect(v.headline).toBe('Waiting at Approve the expense');
    expect(v.node).toBe('Approve');
  });

  it('falls back to the node id when the model never named the step', () => {
    const asking = run([], { outcome: 'needs_answer', awaitingNode: 'Activity_1x2y', awaitingLabel: null });
    expect(verdict(asking).headline).toBe('Waiting at Activity_1x2y');
  });

  it('separates the model being wrong from the process asking for something', () => {
    const broken = run([], {
      outcome: 'incident',
      incidents: [{ node: 'Gateway', message: 'Nothing matched at "Over £5,000?"' }],
    });
    expect(verdict(broken).tone).toBe('bad');
    expect(verdict(broken).node).toBe('Gateway');
  });

  it('says a real run would never pick a branch on its own', () => {
    const broken = run([], {
      outcome: 'incident',
      incidents: [{ node: 'Gateway', message: 'Nothing matched' }],
    });
    expect(verdict(broken).detail).toContain('never picks a branch on its own');
  });

  it('prefers the server hint over the standing explanation when there is one', () => {
    const broken = run([], {
      outcome: 'incident',
      incidents: [{ node: 'Gateway', message: 'Nothing matched', hint: 'Add a default flow.' }],
    });
    expect(verdict(broken).detail).toBe('Add a default flow.');
  });

  it('is quietly good when the run finished clean', () => {
    expect(verdict(run([]))).toMatchObject({ tone: 'good', headline: 'Finished at End' });
  });
});
