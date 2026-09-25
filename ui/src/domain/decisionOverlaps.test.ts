import { describe, expect, it } from 'bun:test';

import { findOverlaps } from './decisionOverlaps';
import type { DecisionInputColumn, DecisionOutputColumn, DecisionRuleRow } from './decisionTable';

const amount: DecisionInputColumn = { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' };
const urgent: DecisionInputColumn = { id: 'i2', label: 'Urgent', expression: 'urgent', type: 'boolean' };
const outputs: DecisionOutputColumn[] = [{ id: 'o1', label: 'Band', name: 'band', type: 'string' }];

function rule(condition: string, result = 'X'): DecisionRuleRow {
  return { id: `${condition}-${result}`, input_entries: [condition], output_entries: [result] };
}

function messages(hitPolicy: string, rules: DecisionRuleRow[], columns = [amount]): string[] {
  return findOverlaps(hitPolicy, columns, outputs, rules).map((problem) => problem.message);
}

/**
 * A catch-all line collides with every other line. It is reported once, in
 * words that say so, rather than once for every line it collides with.
 */
describe('findOverlaps and catch-all lines', () => {
  it('leaves the lines below a catch-all to its own warning when the first match wins', () => {
    // Line 3 is inside line 1 as well, but the catch-all already hides it.
    expect(messages('FIRST', [rule('> 10'), rule('-'), rule('> 20'), rule('> 30')])).toEqual([
      'Line 2 matches everything, so no line below it can ever be reached. Catch-all lines belong last.',
    ]);
  });

  it('names a catch-all once under UNIQUE, not once per line it overlaps', () => {
    const found = messages('UNIQUE', [rule('< 5'), rule('-'), rule('> 10')]);
    expect(found).toHaveLength(1);
    expect(found[0]).toStartWith('Line 2 matches everything');
  });

  it('names two catch-alls that disagree once under ANY', () => {
    const found = messages('ANY', [rule('-', 'LOW'), rule('', 'HIGH')]);
    expect(found).toEqual([
      'Line 1 matches everything, so it applies alongside every other line, and line 2 gives a different result. Lines that apply together must agree, so those cases fail the decision.',
    ]);
  });
});

describe('findOverlaps', () => {
  it('reads yes/no columns', () => {
    expect(messages('UNIQUE', [rule('true'), rule('false')], [urgent])).toEqual([]);
    expect(messages('UNIQUE', [rule('true'), rule('true')], [urgent])).toEqual([
      'Lines 1 and 2 both apply when Urgent is yes, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
    ]);
  });

  it('leaves out a line that can never match anything', () => {
    // [10..1] is empty: nothing is at least 10 and at most 1.
    expect(messages('UNIQUE', [rule('[10..1]'), rule('> 0')])).toEqual([]);
    expect(messages('FIRST', [rule('> 0'), rule('[10..1]')])).toEqual([]);
  });

  it('finds the line that hides another, not merely one before it', () => {
    expect(messages('FIRST', [rule('< 5'), rule('> 10'), rule('> 20')])).toEqual([
      'Line 3 can never be reached: line 2 comes before it and applies to every case it does. Move it above line 2, or remove it.',
    ]);
  });

  it('keeps the warning about a line written twice where overlapping is allowed', () => {
    expect(messages('COLLECT', [rule('> 10', 'A'), rule('>10', 'B')])).toEqual([
      'Lines 1 and 2 test the same conditions.',
    ]);
    expect(messages('COLLECT', [rule('> 10'), rule('> 20')])).toEqual([]);
  });

  it('says nothing about a table of one line', () => {
    expect(messages('UNIQUE', [rule('-')])).toEqual([]);
  });
});
