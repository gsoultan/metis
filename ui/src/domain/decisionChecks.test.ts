import { describe, expect, it } from 'bun:test';

import { coverageCheckKey, coverageFor, overlapCheckKey, overlapsFor } from './decisionChecks';
import { findCoverageGaps } from './decisionCoverage';
import { findOverlaps } from './decisionOverlaps';
import type { DecisionInputColumn, DecisionOutputColumn, DecisionRuleRow } from './decisionTable';

const inputs: DecisionInputColumn[] = [
  { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' },
  { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' },
];
const outputs: DecisionOutputColumn[] = [{ id: 'o1', label: 'Band', name: 'band', type: 'string' }];
const rules: DecisionRuleRow[] = [
  { id: 'r1', input_entries: ['> 10', '"GOLD"'], output_entries: ['HIGH'], description: 'big spenders' },
  { id: 'r2', input_entries: ['> 20', '-'], output_entries: ['LOW'], description: '' },
];

const withResult = (line: number, result: string) =>
  rules.map((rule, index) => (index === line ? { ...rule, output_entries: [result] } : rule));
const withNote = (line: number, note: string) =>
  rules.map((rule, index) => (index === line ? { ...rule, description: note } : rule));
const withCondition = (line: number, condition: string) =>
  rules.map((rule, index) => (index === line ? { ...rule, input_entries: [condition, rule.input_entries[1]] } : rule));

/**
 * The editor keeps a check's answer for as long as its key is unchanged, so the
 * key is what decides when the check runs again: never for a keystroke in a
 * result or a note, always for one in a condition.
 */
describe('overlapCheckKey', () => {
  const key = (hitPolicy: string, lines = rules, columns = inputs) => overlapCheckKey(hitPolicy, columns, outputs, lines);

  it('does not change with a result, a note or a line id where the policy does not compare results', () => {
    for (const hitPolicy of ['UNIQUE', 'FIRST', 'COLLECT']) {
      expect(key(hitPolicy, withResult(0, 'MEDIUM'))).toBe(key(hitPolicy));
      expect(key(hitPolicy, withNote(1, 'why'))).toBe(key(hitPolicy));
      expect(key(hitPolicy, rules.map((rule) => ({ ...rule, id: `${rule.id}-moved` })))).toBe(key(hitPolicy));
    }
  });

  it('changes with a result where the lines that apply together must agree', () => {
    expect(key('ANY', withResult(0, 'MEDIUM'))).not.toBe(key('ANY'));
    expect(key('ANY', withNote(1, 'why'))).toBe(key('ANY'));
  });

  it('changes with a condition, a column, the order of the lines and the policy', () => {
    expect(key('UNIQUE', withCondition(0, '> 11'))).not.toBe(key('UNIQUE'));
    expect(key('UNIQUE', rules, [{ ...inputs[0], type: 'string' }, inputs[1]])).not.toBe(key('UNIQUE'));
    expect(key('UNIQUE', rules, [{ ...inputs[0], label: 'Total' }, inputs[1]])).not.toBe(key('UNIQUE'));
    expect(key('UNIQUE', [...rules].reverse())).not.toBe(key('UNIQUE'));
    expect(key('FIRST')).not.toBe(key('UNIQUE'));
  });

  it('runs the check on exactly the table it describes', () => {
    for (const hitPolicy of ['UNIQUE', 'FIRST', 'ANY', 'COLLECT']) {
      expect(overlapsFor(key(hitPolicy))).toEqual(findOverlaps(hitPolicy, inputs, outputs, rules));
    }
    expect(overlapsFor(key('UNIQUE')).map((problem) => problem.message)).toEqual([
      'Lines 1 and 2 both apply when Amount is 21 and Tier is GOLD, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
    ]);
  });
});

describe('coverageCheckKey', () => {
  it('changes with the conditions and the columns only', () => {
    const key = coverageCheckKey(inputs, rules);
    expect(coverageCheckKey(inputs, withResult(0, 'MEDIUM'))).toBe(key);
    expect(coverageCheckKey(inputs, withNote(0, 'why'))).toBe(key);
    expect(coverageCheckKey(inputs, withCondition(1, '>= 20'))).not.toBe(key);
    expect(coverageCheckKey([inputs[0], { ...inputs[1], label: 'Level' }], rules)).not.toBe(key);
  });

  it('runs the check on exactly the table it describes', () => {
    expect(coverageFor(coverageCheckKey(inputs, rules))).toEqual(findCoverageGaps(inputs, rules));
  });
});
