import { describe, expect, it } from 'bun:test';

import { cellMatcher, columnSamples, understandsCell } from './decisionCoverage';
import type { DecisionInputColumn } from './decisionTable';
import {
  WorkBudget,
  firstShared,
  isEmpty,
  isSubset,
  numberSampleSet,
  samplePositions,
  sampleSet,
  type SampleSet,
} from './sampleSets';

const amount: DecisionInputColumn = { id: 'i1', label: 'Amount', expression: 'amount', type: 'number' };

/**
 * A number cell is asked once per stretch between the numbers it mentions,
 * rather than once per sample, and that is only right if its answer cannot
 * change inside a stretch. Every notation the check reads in a number column is
 * held to asking every sample.
 */
describe('numberSampleSet', () => {
  const cells = [
    '-', '', '> 10', '>= 10', '< 10', '<= 10', '!= 10', '= 10', '10', '-5', '> -5', '5.5', '< 0.25',
    '[1..10]', ']1..10]', '[1..10[', ']1..10[', '[5..15]', ']10..20[', '[0..100]', '10, 20', '20, 5.5, -5',
    'not(> 10)', 'not(10, 20)', 'not([1..10])', '"10"', 'GOLD', 'true', '[10..1]',
  ];
  const samples = columnSamples(amount, cells);
  const positions = samplePositions(samples);

  it.each(cells)('reads %p exactly as asking every sample does', (cell) => {
    expect(understandsCell(cell, 'number')).toBe(true);
    const everySample = sampleSet(samples, cellMatcher(cell, 'number'));
    const byStretch = numberSampleSet(samples, positions, cell, cellMatcher(cell, 'number'));
    expect([...byStretch.bits]).toEqual([...everySample.bits]);
    expect(isEmpty(byStretch)).toBe(isEmpty(everySample));
    if (!isEmpty(everySample)) expect([byStretch.first, byStretch.last]).toEqual([everySample.first, everySample.last]);
  });

  it('fills whole words across a long stretch', () => {
    const many = Array.from({ length: 200 }, (_, i) => `[${i}..${i + 1}[`);
    const wide = columnSamples(amount, [...many, '> 7']);
    const set = numberSampleSet(wide, samplePositions(wide), '> 7', cellMatcher('> 7', 'number'));
    expect([...set.bits]).toEqual([...sampleSet(wide, cellMatcher('> 7', 'number')).bits]);
  });
});

describe('comparing two sets', () => {
  const setOf = (size: number, members: number[]): SampleSet => {
    const samples = Array.from({ length: size }, (_, i) => ({ value: i, label: String(i), standsFor: '', condition: '' }));
    return sampleSet(samples, (value) => members.includes(value as number));
  };
  const plenty = () => new WorkBudget(Number.POSITIVE_INFINITY);

  it('finds the first sample two sets share, or none', () => {
    expect(firstShared(setOf(100, [3, 70]), setOf(100, [70, 99]), plenty())).toBe(70);
    expect(firstShared(setOf(100, [3]), setOf(100, [99]), plenty())).toBe(-1);
  });

  it('knows when one set holds another', () => {
    expect(isSubset(setOf(100, [40, 41]), setOf(100, [1, 40, 41, 90]), plenty())).toBe(true);
    expect(isSubset(setOf(100, [40, 95]), setOf(100, [40, 41]), plenty())).toBe(false);
    expect(isSubset(setOf(100, []), setOf(100, [1]), plenty())).toBe(true);
  });

  it('charges the words it looks at, and one for a pair its spans keep apart', () => {
    const budget = new WorkBudget(3);
    firstShared(setOf(100, [3]), setOf(100, [99]), budget);
    expect(budget.exhausted).toBe(false);
    firstShared(setOf(100, [3, 90]), setOf(100, [4, 95]), budget);
    expect(budget.exhausted).toBe(true);
  });
});
