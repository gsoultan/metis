import { describe, expect, it } from 'bun:test';

import { findProblems } from './decisionProblems';
import { ANY_VALUE, type DecisionInputColumn, type DecisionOutputColumn, type DecisionRuleRow } from './decisionTable';

const inputs: DecisionInputColumn[] = [
  { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' },
];
const outputs: DecisionOutputColumn[] = [
  { id: 'o1', label: 'Severity', name: 'severity', type: 'string' },
];

function rule(conditions: string[], results: string[]): DecisionRuleRow {
  return { id: `${conditions.join()}-${results.join()}`, input_entries: conditions, output_entries: results };
}

/**
 * PRIORITY and OUTPUT ORDER rank by the result column's list of allowed values
 * and refuse to run without it. Catching that here is the difference between a
 * message in the editor and a failed process instance.
 */
describe('findProblems', () => {
  it('refuses a ranking policy with nothing to rank by', () => {
    const problems = findProblems('PRIORITY', inputs, outputs, [rule(['> 10'], ['HIGH'])]);
    expect(problems.some((p) => p.severity === 'error' && p.message.includes('list of allowed values'))).toBe(true);
  });

  it('accepts it once the list is there', () => {
    const ranked: DecisionOutputColumn[] = [{ ...outputs[0], values: ['HIGH', 'LOW'] }];
    const problems = findProblems('PRIORITY', inputs, ranked, [rule(['> 10'], ['HIGH'])]);
    expect(problems.filter((p) => p.severity === 'error')).toEqual([]);
  });

  it('points out a catch-all line that hides everything below it', () => {
    const problems = findProblems('FIRST', inputs, outputs, [
      rule([ANY_VALUE], ['LOW']),
      rule(['> 10'], ['HIGH']),
    ]);
    expect(problems.some((p) => p.message.includes('matches everything'))).toBe(true);
  });

  it('treats two identical lines as fatal under UNIQUE and a smell otherwise', () => {
    const duplicated = [rule(['> 10'], ['HIGH']), rule(['> 10'], ['LOW'])];
    expect(findProblems('UNIQUE', inputs, outputs, duplicated).some((p) => p.severity === 'error')).toBe(true);
    expect(findProblems('FIRST', inputs, outputs, duplicated).some((p) => p.severity === 'warning')).toBe(true);
  });

  it('names a result column that nothing downstream can read', () => {
    const nameless: DecisionOutputColumn[] = [{ ...outputs[0], name: '' }];
    expect(findProblems('FIRST', inputs, nameless, [rule(['> 10'], ['HIGH'])]).some((p) => p.severity === 'error')).toBe(
      true,
    );
  });
});

/**
 * A line with no conditions matches every case. What that costs depends on the
 * hit policy: it hides the lines below it only when the first match wins. The
 * warning used to give that answer under every policy.
 */
describe('a catch-all line', () => {
  const catchAllFirst = [rule([ANY_VALUE], ['LOW']), rule(['> 10'], ['HIGH'])];
  const ranked: DecisionOutputColumn[] = [{ ...outputs[0], values: ['HIGH', 'LOW'] }];
  const aboutCatchAll = (hitPolicy: string, rules: DecisionRuleRow[]) =>
    findProblems(hitPolicy, inputs, ranked, rules).filter((p) => p.message.includes('matches everything'));

  it('hides every line below it when the first match wins', () => {
    expect(aboutCatchAll('FIRST', catchAllFirst)).toEqual([
      {
        severity: 'warning',
        message: 'Line 1 matches everything, so no line below it can ever be reached. Catch-all lines belong last.',
      },
    ]);
  });

  it('hides nothing when the table ranks or collects its matches', () => {
    for (const hitPolicy of ['PRIORITY', 'COLLECT', 'RULE ORDER', 'OUTPUT ORDER']) {
      expect(aboutCatchAll(hitPolicy, catchAllFirst)).toEqual([]);
    }
  });

  it('fails the decision under UNIQUE wherever it sits, because it applies alongside every other line', () => {
    const last = [rule(['> 10'], ['HIGH']), rule([ANY_VALUE], ['LOW'])];
    const problems = aboutCatchAll('UNIQUE', last);
    expect(problems).toHaveLength(1);
    expect(problems[0].severity).toBe('error');
    expect(problems[0].message).toStartWith('Line 2 matches everything, so it applies alongside every other line');
  });

  it('must agree with the lines it applies alongside under ANY', () => {
    const problems = aboutCatchAll('ANY', catchAllFirst);
    expect(problems).toHaveLength(1);
    expect(problems[0].severity).toBe('error');
    expect(problems[0].message).toContain('line 2 gives a different result');

    expect(aboutCatchAll('ANY', [rule([ANY_VALUE], ['LOW']), rule(['> 10'], ['"LOW"'])])).toEqual([]);
  });
});
