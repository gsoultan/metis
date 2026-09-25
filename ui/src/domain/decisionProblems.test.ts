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

/**
 * Two lines collide when they can both apply to one case, not only when their
 * text is the same. `> 10` and `> 20` both apply to 25; under UNIQUE that fails
 * the decision at runtime, and the table used to pass the check.
 */
describe('lines that apply to the same case', () => {
  const tier: DecisionInputColumn = { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' };
  const bigAndBigger = [rule(['> 10'], ['HIGH']), rule(['> 20'], ['LOW'])];
  const errors = (hitPolicy: string, rules: DecisionRuleRow[], columns = inputs) =>
    findProblems(hitPolicy, columns, outputs, rules).filter((p) => p.severity === 'error');

  it('fail the table under UNIQUE, with a case that shows it', () => {
    expect(errors('UNIQUE', bigAndBigger)).toEqual([
      {
        severity: 'error',
        message:
          'Lines 1 and 2 both apply when Amount is 21, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
      },
    ]);
  });

  it('fail the table under ANY only when they give different results', () => {
    expect(errors('ANY', bigAndBigger)).toEqual([
      {
        severity: 'error',
        message:
          'Lines 1 and 2 both apply when Amount is 21 but give different results. Lines that apply together must agree, so the decision fails there.',
      },
    ]);
    expect(errors('ANY', [rule(['> 10'], ['HIGH']), rule(['> 20'], ['"HIGH"'])])).toEqual([]);
  });

  it('are a warning under FIRST when an earlier line hides a later one entirely', () => {
    expect(findProblems('FIRST', inputs, outputs, bigAndBigger)).toContainEqual({
      severity: 'warning',
      message: 'Line 2 can never be reached: line 1 comes before it and applies to every case it does. Move it above line 1, or remove it.',
    });
    // The other way round, the narrower line comes first and both are reached.
    const reachable = findProblems('FIRST', inputs, outputs, [...bigAndBigger].reverse());
    expect(reachable.filter((p) => p.message.includes('never be reached'))).toEqual([]);
  });

  it('are only lines that overlap in every column', () => {
    const gold = rule(['> 10', '"GOLD"'], ['HIGH']);
    const silver = rule(['> 20', '"SILVER"'], ['LOW']);
    expect(errors('UNIQUE', [gold, silver], [inputs[0], tier])).toEqual([]);

    const goldToo = rule(['> 20', 'not("SILVER")'], ['LOW']);
    expect(errors('UNIQUE', [gold, goldToo], [inputs[0], tier]).map((p) => p.message)).toEqual([
      'Lines 1 and 2 both apply when Amount is 21 and Tier is GOLD, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
    ]);
  });

  it('leave touching ranges alone', () => {
    expect(errors('UNIQUE', [rule(['<= 10'], ['LOW']), rule(['> 10'], ['HIGH'])])).toEqual([]);
    expect(errors('UNIQUE', [rule(['[1..10['], ['LOW']), rule(['[10..20]'], ['HIGH'])])).toEqual([]);
  });

  /**
   * A false alarm teaches people to ignore the check, so a cell the matcher
   * cannot read makes the answer unknown, and unknown is not reported. The
   * same text twice still overlaps, whatever it means.
   */
  it('say nothing about notation they cannot read', () => {
    expect(errors('UNIQUE', [rule(['sum(items) > 10'], ['HIGH']), rule(['> 5'], ['LOW'])])).toEqual([]);
    expect(errors('UNIQUE', [rule(['sum(items) > 10'], ['HIGH']), rule(['sum(items) > 10'], ['LOW'])])).toHaveLength(1);
    // A column that is read proves them apart, whatever the other one says.
    const apart = [rule(['< 5', 'lower(name) = "x"'], ['HIGH']), rule(['> 5', 'lower(name) = "y"'], ['LOW'])];
    expect(errors('UNIQUE', apart, [inputs[0], tier])).toEqual([]);
  });

  it('are listed a few at a time, with a count of the rest', () => {
    const everyLineOverlaps = Array.from({ length: 5 }, (_, i) => rule([`> ${i}`], [`R${i}`]));
    const problems = errors('UNIQUE', everyLineOverlaps);
    expect(problems).toHaveLength(6);
    expect(problems[5].message).toBe('5 more pairs of lines overlap the same way.');
  });
});
