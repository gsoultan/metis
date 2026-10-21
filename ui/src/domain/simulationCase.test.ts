import { describe, expect, it } from 'bun:test';

import {
  caseFingerprint,
  casePassed,
  checkExpectation,
  expectationFromRun,
  fromWireCase,
  hasExpectation,
  toWireCase,
  type Expectation,
} from './simulationCase';
import type { Scenario } from './simulationScenario';
import type { SimulationRun } from './simulationTrace';

function run(overrides: Partial<SimulationRun>): SimulationRun {
  return {
    runId: 'r1',
    definition: { key: 'expense-approval', version: 3, id: 'd1' },
    outcome: 'completed',
    endedAtNode: 'approved',
    awaitingNode: null,
    awaitingLabel: null,
    virtualDurationMs: 0,
    variables: { amount: 4200, approver: 'sarah', meta: { region: 'EU', tier: 2 } },
    incidents: [],
    steps: [],
    ...overrides,
  };
}

const labels: Record<string, string> = { approved: 'Approved', rejected: 'Rejected' };
const label = (id: string) => labels[id] ?? id;

function expectation(overrides: Partial<Expectation>): Expectation {
  return { outcome: '', endsAt: '', variables: [], ...overrides };
}

describe('checkExpectation', () => {
  it('passes a run that matches every part', () => {
    const check = checkExpectation(
      run({}),
      expectation({
        outcome: 'completed',
        endsAt: 'approved',
        variables: [
          { id: 'a', name: 'approver', value: 'sarah' },
          { id: 'b', name: 'meta', value: '{"tier": 2, "region": "EU"}' },
        ],
      }),
      label,
    );
    expect(check).toEqual({ passed: true, misses: [] });
  });

  it('names each thing that differs, in the diagram’s words', () => {
    const check = checkExpectation(
      run({ endedAtNode: 'rejected', variables: { amount: 4200 } }),
      expectation({
        outcome: 'completed',
        endsAt: 'approved',
        variables: [
          { id: 'a', name: 'amount', value: '4000' },
          { id: 'b', name: 'approver', value: 'sarah' },
        ],
      }),
      label,
    );
    expect(check.passed).toBe(false);
    expect(check.misses).toEqual([
      'It ended at Rejected, not Approved.',
      'amount is 4200, not 4000.',
      'approver was expected to be sarah, and was never set.',
    ]);
  });

  it('reads a stop to ask as ending where it stopped', () => {
    const asking = run({ outcome: 'needs_answer', endedAtNode: null, awaitingNode: 'charge' });
    expect(checkExpectation(asking, expectation({ outcome: 'needs_answer', endsAt: 'charge' })).passed).toBe(true);
    expect(checkExpectation(asking, expectation({ outcome: 'completed' })).misses).toEqual([
      'It was expected to finish, and did not.',
    ]);
  });
});

describe('expectationFromRun', () => {
  it('takes how and where it ended, and leaves the values to the person', () => {
    expect(expectationFromRun(run({}))).toEqual({ outcome: 'completed', endsAt: 'approved', variables: [] });
  });

  it('never expects running out of steps', () => {
    expect(expectationFromRun(run({ outcome: 'step_budget', endedAtNode: null })).outcome).toBe('');
  });
});

describe('casePassed', () => {
  const scenario: Scenario = { id: 'c1', name: 'Over limit', variables: [], answers: [], seed: 1 };

  it('falls back to "finished cleanly" for a case that expects nothing', () => {
    expect(hasExpectation(scenario.expect)).toBe(false);
    expect(casePassed(scenario, run({}))).toBe(true);
    expect(casePassed(scenario, run({ outcome: 'incident' }))).toBe(false);
  });

  it('lets a case expect an incident, and pass when it gets one', () => {
    const expectsRefusal = { ...scenario, expect: expectation({ outcome: 'incident' }) };
    expect(casePassed(expectsRefusal, run({ outcome: 'incident' }))).toBe(true);
    expect(casePassed(expectsRefusal, run({}))).toBe(false);
  });
});

describe('the wire', () => {
  const scenario: Scenario = {
    id: 'local-1',
    name: '  Over limit  ',
    variables: [{ id: 'v', name: 'amount', value: '4200' }],
    answers: [
      { nodeId: 'approve', kind: 'person', actor: 'sarah', after: 'PT1H' },
      { nodeId: 'charge', kind: 'service', outcome: 'fail', returns: [], failureCode: 'CARD_DECLINED' },
    ],
    seed: 7,
    expect: expectation({ outcome: 'completed', endsAt: 'approved', variables: [{ id: 'e', name: 'paid', value: 'true' }] }),
  };

  it('round-trips a case through what the server stores', () => {
    const wire = toWireCase(scenario);
    expect(wire.name).toBe('Over limit');
    expect(wire.scenario.variables).toEqual({ amount: 4200 });
    expect(wire.expect).toEqual({ outcome: 'completed', ends_at: 'approved', variables: { paid: true } });

    const back = fromWireCase({ ...wire, id: 'srv-1' });
    expect(back.savedId).toBe('srv-1');
    expect(back.answers[1]).toEqual({ nodeId: 'charge', kind: 'service', outcome: 'fail', returns: [], failureCode: 'CARD_DECLINED' });
    expect(caseFingerprint(back)).toBe(caseFingerprint({ ...scenario, name: 'Over limit' }));
  });

  it('sends no expectation fields for a case that expects nothing', () => {
    expect(toWireCase({ ...scenario, expect: undefined }).expect).toEqual({
      outcome: undefined,
      ends_at: undefined,
      variables: undefined,
    });
  });
});
