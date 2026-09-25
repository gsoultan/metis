import { describe, expect, it } from 'bun:test';

import type { DecisionInputColumn, DecisionRuleRow } from './decisionTable';
import {
  describeMatchedLines,
  matchedLines,
  tableFingerprint,
  trialTarget,
  trialValueOf,
  trialVariables,
  withTrialValue,
  type TrialOutcome,
} from './decisionTrial';

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

/**
 * The lines Try it highlights. They were found by position and never cleared:
 * move a line, or edit the table after running, and the highlight sat on
 * whatever line now had that number.
 */
describe('matchedLines', () => {
  const line = (id: string): DecisionRuleRow => ({ id, input_entries: ['-'], output_entries: ['x'] });
  const outcome = (over: Partial<TrialOutcome>): TrialOutcome => ({
    values: {},
    ruleIds: [],
    positions: [],
    table: 'as run',
    ranAsShown: true,
    ...over,
  });

  it('finds the lines that decided by id, wherever they now sit', () => {
    // Stored as a, b, c; c decided. On screen, before saving, c was moved to the top.
    const decided = outcome({ ruleIds: ['c'], positions: [2], ranAsShown: false });
    expect(matchedLines(decided, [line('c'), line('a'), line('b')], 'as run')).toEqual([0]);
  });

  it('clears once the table has changed since it ran', () => {
    const decided = outcome({ ruleIds: ['b'], positions: [1] });
    expect(matchedLines(decided, [line('a'), line('b')], 'edited since')).toEqual([]);
  });

  it('uses positions only for lines stored without an id, and only when the stored table was on screen', () => {
    const rows = [line(''), line('')];
    expect(matchedLines(outcome({ positions: [1] }), rows, 'as run')).toEqual([1]);
    expect(matchedLines(outcome({ positions: [1], ranAsShown: false }), rows, 'as run')).toEqual([]);
  });

  it('highlights nothing before anything has run', () => {
    expect(matchedLines(null, [line('a')], 'as run')).toEqual([]);
  });
});

/**
 * What a Try-it answer is an answer about. The name and the examples do not
 * change what a table decides, so editing them does not make an answer stale.
 */
describe('tableFingerprint', () => {
  const payload = {
    name: 'Discount',
    key: 'discount',
    hit_policy: 'FIRST',
    inputs: [{ id: 'i1', label: 'Amount', expression: 'amount', type: 'number' }],
    outputs: [{ id: 'o1', label: 'Discount', name: 'discount', type: 'number' }],
    rules: [{ id: 'r1', inputs: ['> 10'], outputs: [5] }],
    tests: [],
  };

  it('ignores the name and the examples', () => {
    expect(tableFingerprint({ ...payload, name: 'Renamed', tests: [{ id: 't', name: 'x' }] })).toBe(
      tableFingerprint(payload),
    );
  });

  it('changes with any line', () => {
    expect(tableFingerprint({ ...payload, rules: [{ id: 'r1', inputs: ['> 20'], outputs: [5] }] })).not.toBe(
      tableFingerprint(payload),
    );
  });
});

describe('describeMatchedLines', () => {
  it('names the lines on screen', () => {
    expect(describeMatchedLines([2])).toBe('Line 3 matched');
    expect(describeMatchedLines([1, 4, 6])).toBe('Lines 2, 5 and 7 matched');
  });

  it('still says something matched when the line is no longer on screen', () => {
    expect(describeMatchedLines([])).toBe('A line of the saved version matched');
  });
});
