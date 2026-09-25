import { describe, expect, it } from 'bun:test';

import { cellMatcher, findCoverageGaps, ruleForGap } from './decisionCoverage';
import { findOverlaps } from './decisionOverlaps';
import { ANY_VALUE, type DecisionInputColumn, type DecisionOutputColumn, type DecisionRuleRow } from './decisionTable';

const amount: DecisionInputColumn = { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' };
const tier: DecisionInputColumn = { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' };
const urgent: DecisionInputColumn = { id: 'i3', label: 'Urgent', expression: 'urgent', type: 'boolean' };
const output: DecisionOutputColumn = { id: 'o1', label: 'Band', name: 'band', type: 'string' };

function rule(...cells: string[]): DecisionRuleRow {
  return { id: cells.join('|'), input_entries: cells, output_entries: ['x'] };
}

/**
 * A table is a promise that every case has an answer, and the way that promise
 * breaks is quiet: no line matches, the decision returns nothing, and a process
 * carries on with an empty variable until something downstream fails for an
 * unrelated-looking reason.
 */
describe('findCoverageGaps', () => {
  it('finds the off-by-one at a threshold, which is the commonest gap there is', () => {
    // Under 100, and over 100 — nothing decides exactly 100.
    const report = findCoverageGaps([amount], [rule('< 100'), rule('> 100')]);

    expect(report.gaps).toHaveLength(1);
    expect(report.gaps[0].values).toEqual(['100']);
    expect(report.gaps[0].description).toBe('Nothing decides when Amount is 100');
  });

  it('says nothing about a table that covers everything', () => {
    expect(findCoverageGaps([amount], [rule('< 100'), rule('>= 100')]).gaps).toEqual([]);
    expect(findCoverageGaps([amount], [rule(ANY_VALUE)]).gaps).toEqual([]);
  });

  it('finds a missing catch-all on a text column', () => {
    const report = findCoverageGaps([tier], [rule('GOLD'), rule('SILVER')]);
    expect(report.gaps.map((gap) => gap.values[0])).toEqual(['anything else']);
  });

  it('is satisfied by a catch-all', () => {
    expect(findCoverageGaps([tier], [rule('GOLD'), rule(ANY_VALUE)]).gaps).toEqual([]);
  });

  it('finds a combination neither column misses on its own', () => {
    // Each column alone is fully covered; the pair GOLD-and-small is not.
    const report = findCoverageGaps(
      [amount, tier],
      [rule('>= 100', 'GOLD'), rule(ANY_VALUE, 'SILVER'), rule('< 100', 'SILVER')],
    );
    const goldSmall = report.gaps.find((gap) => gap.values[1] === 'GOLD' && Number(gap.values[0]) < 100);
    expect(goldSmall).toBeDefined();
  });

  it('covers both sides of a boolean', () => {
    expect(findCoverageGaps([urgent], [rule('true')]).gaps.map((g) => g.values[0])).toEqual(['no']);
    expect(findCoverageGaps([urgent], [rule('true'), rule('false')]).gaps).toEqual([]);
  });

  /**
   * A coverage warning that is wrong teaches people to ignore coverage
   * warnings, so a table containing notation this analysis does not understand
   * is reported as un-analysed rather than guessed at.
   */
  it('refuses to guess at notation it does not understand', () => {
    const report = findCoverageGaps([amount], [rule('sum(items.price) > 10')]);
    expect(report.gaps).toEqual([]);
    expect(report.notAnalysed).toEqual(['Amount']);
  });

  it('reads not( ) only around a condition it can read itself', () => {
    const report = findCoverageGaps([amount], [rule('not(sum(items.price) > 10)')]);
    expect(report.gaps).toEqual([]);
    expect(report.notAnalysed).toEqual(['Amount']);
  });

  it('says when the search was cut short rather than implying the table is fine', () => {
    // Enough distinct boundaries across enough columns to pass the cap.
    const wide: DecisionInputColumn[] = [amount, { ...tier, id: 'i2' }, { ...amount, id: 'i4', label: 'Weight' }];
    const many = Array.from({ length: 8 }, (_, i) => rule(`> ${i * 10}`, `T${i}`, `> ${i * 5}`));

    const report = findCoverageGaps(wide, many);
    expect(report.truncated).toBe(true);
  });

  /**
   * Each column's values used to be cut to the first eight, with nothing said.
   * A column with five thresholds has eleven values worth trying, so the top of
   * the range was never looked at and a table missing it was reported whole.
   */
  it('looks at every threshold a column mentions, not the first few', () => {
    const banded = [rule('< 10'), rule('[10..20['), rule('[20..30['), rule('[30..40['), rule('[40..50[')];
    const report = findCoverageGaps([amount], banded);
    expect(report.gaps.map((gap) => gap.values[0])).toEqual(['50', '51']);
    expect(report.truncated).toBe(false);
  });

  /**
   * The value tried past a threshold was the threshold plus one. When the next
   * threshold is closer than that, the space between the two was never tried:
   * `<= 10` and `>= 11` leave an amount of 10.50 undecided.
   */
  it('tries a value between two thresholds, however close they are', () => {
    expect(findCoverageGaps([amount], [rule('<= 10'), rule('>= 11')]).gaps.map((gap) => gap.values[0])).toEqual([
      '10.5',
    ]);
    expect(findCoverageGaps([amount], [rule('<= 10'), rule('> 10.5')]).gaps.map((gap) => gap.values[0])).toEqual([
      '10.25',
      '10.5',
    ]);
  });

  it('says it stopped short when it stops at the most gaps it will report', () => {
    // Twelve named tiers, none of them decided: more gaps than are reported.
    const tiers = Array.from({ length: 12 }, (_, i) => `"T${i}"`).join(', ');
    const report = findCoverageGaps([tier], [rule(`not(${tiers})`)]);
    expect(report.gaps).toHaveLength(10);
    expect(report.truncated).toBe(true);
  });

  it('reports nothing for a table with no lines', () => {
    expect(findCoverageGaps([amount], []).gaps).toEqual([]);
  });
});

/**
 * The matcher is a partial reimplementation of the unary tests the engine runs.
 * Where it disagrees with the engine, the analysis built on it is wrong — so
 * the notations it claims to understand are pinned here.
 */
describe('cellMatcher', () => {
  const cellMatches = (cell: string, value: string | number | boolean, type: string) => cellMatcher(cell, type)(value);

  it('reads the comparisons', () => {
    expect(cellMatches('> 10', 11, 'number')).toBe(true);
    expect(cellMatches('> 10', 10, 'number')).toBe(false);
    expect(cellMatches('>= 10', 10, 'number')).toBe(true);
    expect(cellMatches('< 10', 9, 'number')).toBe(true);
    expect(cellMatches('<= 10', 10, 'number')).toBe(true);
    expect(cellMatches('!= 10', 11, 'number')).toBe(true);
    expect(cellMatches('10', 10, 'number')).toBe(true);
  });

  it('reads both spellings of an open range', () => {
    expect(cellMatches('[1..10]', 1, 'number')).toBe(true);
    expect(cellMatches('[1..10]', 10, 'number')).toBe(true);
    expect(cellMatches(']1..10]', 1, 'number')).toBe(false);
    expect(cellMatches('[1..10[', 10, 'number')).toBe(false);
  });

  it('reads wildcards, lists and negation', () => {
    expect(cellMatches(ANY_VALUE, 'anything', 'string')).toBe(true);
    expect(cellMatches('', 'anything', 'string')).toBe(true);
    expect(cellMatches('"A", "B"', 'B', 'string')).toBe(true);
    expect(cellMatches('"A", "B"', 'C', 'string')).toBe(false);
    expect(cellMatches('not("A")', 'B', 'string')).toBe(true);
    expect(cellMatches('not("A")', 'A', 'string')).toBe(false);
  });

  it('reads bare words as text, the way the engine does in a cell', () => {
    expect(cellMatches('GOLD', 'GOLD', 'string')).toBe(true);
    expect(cellMatches('GOLD', 'SILVER', 'string')).toBe(false);
  });

  /**
   * The engine never finds a string equal to a number, and reads only
   * lower-case true and false as yes and no. The matcher said otherwise, so the
   * checks built on it reported overlaps, and coverage, the engine does not have.
   */
  it('reads a literal with its type, as the engine does', () => {
    expect(cellMatches('"10"', 10, 'number')).toBe(false);
    expect(cellMatches('10', '10', 'string')).toBe(false);
    expect(cellMatches('TRUE', true, 'boolean')).toBe(false);
    expect(cellMatches('10, 20', 20, 'number')).toBe(true);
    expect(cellMatches('"A", B', 'B', 'string')).toBe(true);
  });

  it('reads booleans', () => {
    expect(cellMatches('true', true, 'boolean')).toBe(true);
    expect(cellMatches('true', false, 'boolean')).toBe(false);
    expect(cellMatches('false', false, 'boolean')).toBe(true);
  });
});

/**
 * A gap is a stretch of cases, not the one value that found it. `<= 10` and
 * `> 20` leave everything above 10 and below 20 undecided, and 20 itself. The
 * gap has to say so, because the line that fills it has to cover all of it.
 */
describe('a coverage gap', () => {
  it('says which stretch of cases nothing decides', () => {
    const report = findCoverageGaps([amount], [rule('<= 10'), rule('> 20')]);
    expect(report.gaps.map((gap) => gap.description)).toEqual([
      'Nothing decides when Amount is more than 10 but less than 20',
      'Nothing decides when Amount is 20',
    ]);
  });

  it('says it for each column of a combination', () => {
    const report = findCoverageGaps([amount, tier], [rule('< 100', '"GOLD"'), rule('>= 100', ANY_VALUE)]);
    expect(report.gaps.map((gap) => gap.description)).toEqual([
      'Nothing decides when Amount is less than 100 and Tier is anything else',
    ]);
  });
});

describe('ruleForGap', () => {
  const lineFor = (inputs: DecisionInputColumn[], rules: DecisionRuleRow[], gap = 0) =>
    ruleForGap(findCoverageGaps(inputs, rules).gaps[gap], 'new', 1);

  it('writes the conditions that decide exactly the missing cases', () => {
    expect(lineFor([amount], [rule('< 100'), rule('> 100')]).input_entries).toEqual(['100']);
    expect(lineFor([amount], [rule('<= 10'), rule('> 20')]).input_entries).toEqual([']10..20[']);
    expect(lineFor([amount], [rule('>= 10')]).input_entries).toEqual(['< 10']);
    expect(lineFor([amount], [rule('<= 10')]).input_entries).toEqual(['> 10']);
    expect(lineFor([urgent], [rule('true')]).input_entries).toEqual(['false']);
  });

  it('covers a missing catch-all with everything the column does not name', () => {
    expect(lineFor([tier], [rule('GOLD'), rule('"SILVER"')]).input_entries).toEqual(['not("GOLD", "SILVER")']);
    expect(lineFor([amount, tier], [rule('< 100', '"GOLD"'), rule('>= 100', ANY_VALUE)]).input_entries).toEqual([
      '< 100',
      'not("GOLD")',
    ]);
  });

  it('closes the gap without overlapping any line, so it is safe under every hit policy', () => {
    const tables: [DecisionInputColumn[], DecisionRuleRow[]][] = [
      [[amount], [rule('<= 10'), rule('> 20')]],
      [[tier], [rule('GOLD'), rule('not("GOLD", "BRONZE")')]],
      [[amount, tier], [rule('>= 100', '"GOLD"'), rule(ANY_VALUE, '"SILVER"'), rule('< 100', '"SILVER"')]],
    ];
    for (const [inputs, rules] of tables) {
      const before = findCoverageGaps(inputs, rules);
      const filled = [...rules, ruleForGap(before.gaps[0], 'new', 1)];
      const after = findCoverageGaps(inputs, filled);
      expect(after.gaps.map((gap) => gap.description)).not.toContain(before.gaps[0].description);
      expect(findOverlaps('UNIQUE', inputs, [output], filled.map((line, i) => ({ ...line, output_entries: [`R${i}`] })))).toEqual(
        findOverlaps('UNIQUE', inputs, [output], rules.map((line, i) => ({ ...line, output_entries: [`R${i}`] }))),
      );
    }
  });

  it('leaves the results for the author to fill in', () => {
    const line = ruleForGap(findCoverageGaps([amount], [rule('< 100')]).gaps[0], 'new-line', 2);
    expect(line).toEqual({ id: 'new-line', input_entries: ['100'], output_entries: ['', ''], description: '' });
  });
});

/**
 * `""` is the cell menu's "Empty": text with nothing in it, which the engine
 * matches like any other text. The checks dropped it as if it were no value at
 * all, so a line saying Empty decided nothing they could see: "anything else"
 * was offered as a gap, and the line written for it, `not("GOLD")`, matches
 * the empty text too — two lines for one case, which fails a table where only
 * one may match.
 */
describe('the Empty condition', () => {
  it('is a case the table decides, not a gap', () => {
    const report = findCoverageGaps([tier], [rule('"GOLD"'), rule('""')]);
    expect(report.gaps.map((gap) => gap.description)).toEqual(['Nothing decides when Tier is anything else']);
    expect(ruleForGap(report.gaps[0], 'new', 1).input_entries).toEqual(['not("GOLD", "")']);
  });

  it('is a gap of its own when no line decides it', () => {
    const report = findCoverageGaps([tier], [rule('"GOLD"'), rule('not("GOLD", "")')]);
    expect(report.gaps.map((gap) => gap.description)).toEqual(['Nothing decides when Tier is empty']);
    expect(ruleForGap(report.gaps[0], 'new', 1).input_entries).toEqual(['""']);
  });

  it('is read inside a list and inside not( ), and in either quote', () => {
    expect(findCoverageGaps([tier], [rule('"GOLD", \'\''), rule('not("GOLD", "")')]).gaps).toEqual([]);
  });

  it('is text like any other: a quoted dash is the text "-", not "any value"', () => {
    const report = findCoverageGaps([tier], [rule('"-"')]);
    expect(ruleForGap(report.gaps[0], 'new', 1).input_entries).toEqual(['not("-")']);
  });
});

/**
 * A yes/no column read `true` and `false` and nothing else: `not(true)`,
 * `not(false)` and `true, false` matched no answer at all, though the checks
 * called them readable. So "not yes" was reported as leaving "no" undecided,
 * Add line wrote `false` beside it, and the engine then failed every "no" on
 * two lines where only one may match.
 */
describe('a yes/no column', () => {
  it('is decided by not(true) for no, and by a list for both', () => {
    expect(findCoverageGaps([urgent], [rule('true'), rule('not(true)')]).gaps).toEqual([]);
    expect(findCoverageGaps([urgent], [rule('not(false)'), rule('false')]).gaps).toEqual([]);
    expect(findCoverageGaps([urgent], [rule('true, false')]).gaps).toEqual([]);
  });

  it('still finds the answer nothing decides', () => {
    const report = findCoverageGaps([urgent], [rule('not(false)')]);
    expect(report.gaps.map((gap) => gap.description)).toEqual(['Nothing decides when Urgent is no']);
    expect(ruleForGap(report.gaps[0], 'new', 1).input_entries).toEqual(['false']);
  });

  it('is matched the way the engine matches it', () => {
    const cellMatches = (cell: string, value: boolean) => cellMatcher(cell, 'boolean')(value);
    expect(cellMatches('not(true)', false)).toBe(true);
    expect(cellMatches('not(true)', true)).toBe(false);
    expect(cellMatches('not(false)', true)).toBe(true);
    expect(cellMatches('true, false', false)).toBe(true);
    // Upper case is a bare word, which the engine reads as text.
    expect(cellMatches('TRUE', true)).toBe(false);
    expect(cellMatches('not(TRUE)', true)).toBe(true);
  });

  it('has its overlaps found under not( ) as well', () => {
    const unique = (rules: DecisionRuleRow[]) =>
      findOverlaps('UNIQUE', [urgent], [output], rules.map((line, i) => ({ ...line, output_entries: [`R${i}`] })));
    expect(unique([rule('true'), rule('not(true)')])).toEqual([]);
    expect(unique([rule('not(true)'), rule('not(true)')]).map((problem) => problem.message)).toEqual([
      'Lines 1 and 2 both apply when Urgent is no, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
    ]);
    expect(unique([rule('true, false'), rule('true')]).map((problem) => problem.message)).toEqual([
      'Lines 1 and 2 both apply when Urgent is yes, and only one line may match, so the decision fails there. Narrow one of them so they no longer overlap.',
    ]);
  });
});
