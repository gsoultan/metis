import { describe, expect, it } from 'bun:test';

import { cellMatches, findCoverageGaps } from './decisionCoverage';
import { ANY_VALUE, type DecisionInputColumn, type DecisionRuleRow } from './decisionTable';

const amount: DecisionInputColumn = { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' };
const tier: DecisionInputColumn = { id: 'i2', label: 'Tier', expression: 'tier', type: 'string' };
const urgent: DecisionInputColumn = { id: 'i3', label: 'Urgent', expression: 'urgent', type: 'boolean' };

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

  it('says when the search was cut short rather than implying the table is fine', () => {
    // Enough distinct boundaries across enough columns to pass the cap.
    const wide: DecisionInputColumn[] = [amount, { ...tier, id: 'i2' }, { ...amount, id: 'i4', label: 'Weight' }];
    const many = Array.from({ length: 8 }, (_, i) => rule(`> ${i * 10}`, `T${i}`, `> ${i * 5}`));

    const report = findCoverageGaps(wide, many);
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
describe('cellMatches', () => {
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

  it('reads booleans', () => {
    expect(cellMatches('true', true, 'boolean')).toBe(true);
    expect(cellMatches('true', false, 'boolean')).toBe(false);
    expect(cellMatches('false', false, 'boolean')).toBe(true);
  });
});
