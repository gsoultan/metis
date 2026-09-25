import { describe, expect, it } from 'bun:test';

import type { DecisionInputColumn } from './decisionTable';
import { trialTarget, trialValueOf, trialVariables, withTrialValue } from './decisionTrial';

/**
 * Try it runs a stored table. It used to send the key typed into the editor,
 * with no version: an edited key named another table or none (saving does not
 * change a stored key), and with no version the server answered with the
 * newest table of that key, not necessarily the one on screen.
 */
describe('trialTarget', () => {
  it('runs the table the editor loaded, at the version it loaded', () => {
    expect(trialTarget({ key: 'discount', version: 3 })).toEqual({ key: 'discount', version: 3 });
  });

  it('runs nothing for a table that has never been saved', () => {
    expect(trialTarget(undefined)).toBeNull();
  });
});

/**
 * What was typed into Try it was kept under each column's variable name. Rename
 * the variable and the box emptied while the old value was still sent under
 * the old name; remove the column and its value was sent anyway.
 */
describe('trialVariables', () => {
  const amount: DecisionInputColumn = { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' };
  const tier: DecisionInputColumn = { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' };
  const urgent: DecisionInputColumn = { id: 'i3', label: 'Urgent', expression: 'urgent', type: 'boolean' };

  it('keeps a value with its column when the column reads another variable', () => {
    const typed = withTrialValue({}, amount, '500');
    const renamed = { ...amount, expression: 'order_total' };
    expect(trialValueOf(typed, renamed)).toBe('500');
    expect(trialVariables([renamed], typed)).toEqual({ order_total: 500 });
  });

  it('sends nothing for a column that was removed', () => {
    const typed = withTrialValue(withTrialValue({}, amount, '500'), tier, 'GOLD');
    expect(trialVariables([amount], typed)).toEqual({ amount: 500 });
  });

  it('reads what was typed as its column type says', () => {
    let typed = withTrialValue({}, amount, '500');
    typed = withTrialValue(typed, tier, 'GOLD');
    typed = withTrialValue(typed, urgent, 'true');
    expect(trialVariables([amount, tier, urgent], typed)).toEqual({ amount: 500, tier: 'GOLD', urgent: true });
  });
});
