import { describe, expect, it } from 'bun:test';

import type { DecisionInputColumn, DecisionRuleRow } from './decisionTable';
import {
  describeMatchedLines,
  matchedLines,
  staleNote,
  tableFingerprint,
  trialOutcome,
  trialStanding,
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

  /**
   * A yes/no question left unanswered was sent as "no", and an empty box as
   * empty text: an answer nobody gave. Blank means not given, as it does for
   * the examples saved with the table.
   */
  it('leaves out what was not filled in, rather than answering no', () => {
    expect(trialVariables([amount, tier, urgent], withTrialValue({}, amount, '500'))).toEqual({ amount: 500 });
    expect(trialVariables([amount], withTrialValue({}, amount, '  '))).toEqual({});
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
  // Unless a test says otherwise the stored table was what was on screen.
  const outcome = (over: Partial<TrialOutcome>): TrialOutcome => ({
    values: {},
    ruleIds: [],
    positions: [],
    table: 'as run',
    savedTable: 'as run',
    ...over,
  });

  it('finds the lines that decided by id, wherever they now sit', () => {
    // Stored as a, b, c; c decided. On screen, before saving, c was moved to the top.
    const decided = outcome({ ruleIds: ['c'], positions: [2], savedTable: 'stored' });
    expect(matchedLines(decided, [line('c'), line('a'), line('b')], 'as run', 'stored')).toEqual([0]);
  });

  it('clears once the table has changed since it ran', () => {
    const decided = outcome({ ruleIds: ['b'], positions: [1] });
    expect(matchedLines(decided, [line('a'), line('b')], 'edited since', 'as run')).toEqual([]);
  });

  it('uses positions only for lines stored without an id, and only when the stored table was on screen', () => {
    const rows = [line(''), line('')];
    expect(matchedLines(outcome({ positions: [1] }), rows, 'as run', 'as run')).toEqual([1]);
    expect(matchedLines(outcome({ positions: [1], savedTable: 'stored' }), rows, 'as run', 'stored')).toEqual([]);
  });

  it('highlights nothing before anything has run', () => {
    expect(matchedLines(null, [line('a')], 'as run', 'as run')).toEqual([]);
  });
});

/**
 * Try it runs the stored table. Run it with changes on screen and the answer
 * is the stored table's; save, and that stored table is gone — replaced by
 * what is on screen. The answer stayed up as "what the saved version decides.
 * Your changes are not saved yet", about a table that was no longer saved, and
 * its old lines stayed highlighted.
 */
describe('a Try-it answer after a save', () => {
  const ranWithChangesOnScreen = trialOutcome(
    { result: { values: { band: 'LOW' } }, matchedRules: [0], matchedRuleIds: ['a'] },
    'edited',
    'stored',
  );
  const rows: DecisionRuleRow[] = [{ id: 'a', input_entries: ['-'], output_entries: ['x'] }];

  it('is about the saved version until the table is saved', () => {
    expect(trialStanding(ranWithChangesOnScreen, 'edited', 'stored')).toBe('saved-only');
  });

  it('is stale once the stored table it ran against has been saved over', () => {
    expect(trialStanding(ranWithChangesOnScreen, 'edited', 'edited')).toBe('stale');
    expect(matchedLines(ranWithChangesOnScreen, rows, 'edited', 'edited')).toEqual([]);
  });

  it('says to run it again, not to save, when there is nothing left to save', () => {
    expect(staleNote('edited', 'edited')).toBe(
      'The saved table has changed since this ran, so its answer is no longer shown. Run it again.',
    );
    expect(staleNote('edited again', 'edited')).toBe(
      'The table has changed since this ran, so its answer is no longer shown. Save, and run it again.',
    );
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
