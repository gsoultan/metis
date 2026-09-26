import { describe, expect, it } from 'bun:test';

import type { ApiDecision } from '../services/types';
import { decisionPayload, editorStateFrom } from './decisionSave';

const stored: ApiDecision = {
  id: 'd-1',
  key: 'discount',
  name: 'Discount',
  version: 3,
  hit_policy: 'FIRST',
  inputs: [{ id: 'i1', label: 'Amount', expression: 'amount', type: 'number' }],
  outputs: [{ id: 'o1', label: 'Discount', name: 'discount', type: 'number' }],
  rules: [
    { id: 'r1', inputs: ['> 1000'], outputs: [10], description: 'Big orders' },
    { id: 'r2', inputs: ['-'], outputs: [0], description: '' },
  ],
  tests: [
    { id: 't1', name: 'A big order', inputs: { amount: 5000 }, expected: { discount: 10 } },
    { id: 't2', name: 'A small order', inputs: { amount: 10 }, expected: { discount: 0 } },
  ],
};

describe('decisionPayload', () => {
  it('saves the examples stored with the table, which the save left out', () => {
    const payload = decisionPayload(editorStateFrom(stored));
    expect(payload.tests).toEqual(stored.tests);
  });

  it('sends back what was stored when nothing was edited', () => {
    const payload = decisionPayload(editorStateFrom(stored));
    expect(payload).toMatchObject({
      key: stored.key,
      name: stored.name,
      hit_policy: stored.hit_policy,
      inputs: stored.inputs,
      outputs: stored.outputs,
      rules: stored.rules,
      tests: stored.tests,
    });
  });

  it('saves an example added in the editor', () => {
    const state = editorStateFrom({ ...stored, tests: [] });
    state.tests = [{ id: 't9', name: 'Medium', inputs: { amount: '500' }, expected: { discount: '0' } }];
    expect(decisionPayload(state).tests).toEqual([
      { id: 't9', name: 'Medium', inputs: { amount: 500 }, expected: { discount: 0 } },
    ]);
  });
});
