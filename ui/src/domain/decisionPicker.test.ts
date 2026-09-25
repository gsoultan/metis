import { describe, expect, it } from 'bun:test';

import { decisionOptions } from './decisionPicker';

describe('decisionOptions', () => {
  it('offers each key once, named as the decision is', () => {
    expect(
      decisionOptions(
        [
          { key: 'band', name: 'Credit band' },
          { key: 'band', name: 'Credit band' },
          { key: 'risk', name: '' },
        ],
        '',
      ),
    ).toEqual([
      { value: 'band', label: 'Credit band' },
      { value: 'risk', label: 'risk' },
    ]);
  });

  it('keeps the key a step already names, even when no decision has it', () => {
    expect(decisionOptions([{ key: 'band', name: 'Credit band' }], 'retired')).toEqual([
      { value: 'band', label: 'Credit band' },
      { value: 'retired', label: 'retired' },
    ]);
  });
});
