import { describe, expect, it } from 'bun:test';

import { testsToPayload, type DecisionTestRow } from './decisionTests';
import type { DecisionInputColumn, DecisionOutputColumn } from './decisionTable';

const inputs: DecisionInputColumn[] = [
  { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' },
  { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' },
];
const outputs: DecisionOutputColumn[] = [
  { id: 'o1', label: 'Band', name: 'band', type: 'string' },
  { id: 'o2', label: 'Fee', name: 'fee', type: 'number' },
];

function row(over: Partial<DecisionTestRow> = {}): DecisionTestRow {
  return { id: 't1', name: 'an example', inputs: {}, expected: {}, ...over };
}

describe('testsToPayload', () => {
  it('reads what the author typed as the value they meant', () => {
    const [payload] = testsToPayload(
      [row({ inputs: { amount: '500', tier: 'GOLD' }, expected: { band: 'HIGH', fee: '25' } })],
      inputs,
      outputs,
    );

    expect(payload.inputs).toEqual({ amount: 500, tier: 'GOLD' });
    expect(payload.expected).toEqual({ band: 'HIGH', fee: 25 });
  });

  /**
   * On the input side a blank is "this variable is absent", not "the empty
   * string". On the expectation side it is "do not check this output" — which
   * is what lets a table grow a column without invalidating every example
   * written before it.
   */
  it('omits a blank cell rather than sending an empty string', () => {
    const [payload] = testsToPayload(
      [row({ inputs: { amount: '500', tier: '  ' }, expected: { band: 'HIGH' } })],
      inputs,
      outputs,
    );

    expect(payload.inputs).toEqual({ amount: 500 });
    expect('tier' in payload.inputs).toBe(false);
    expect(payload.expected).toEqual({ band: 'HIGH' });
    expect('fee' in payload.expected).toBe(false);
  });

  it('takes the quotes off, the way a result cell does', () => {
    const [payload] = testsToPayload([row({ expected: { band: '"HIGH"' } })], inputs, outputs);
    expect(payload.expected.band).toBe('HIGH');
  });

  it('keeps the name and id, which is how a result finds its row again', () => {
    const [payload] = testsToPayload([row({ id: 'abc', name: 'a large order' })], inputs, outputs);
    expect(payload.id).toBe('abc');
    expect(payload.name).toBe('a large order');
  });

  it('ignores a column the example knows nothing about', () => {
    const [payload] = testsToPayload([row({ inputs: { removed: 'x' } })], inputs, outputs);
    expect(payload.inputs).toEqual({});
  });
});
