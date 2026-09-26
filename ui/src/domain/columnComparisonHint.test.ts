/**
 * The hint under the decision editor's grid: a condition can name another
 * column to compare with it, which nobody guesses from a grid of literals.
 */
import { describe, expect, it } from 'bun:test';

import { columnComparisonHint, type DecisionInputColumn } from './decisionTable';

const score: DecisionInputColumn = { id: 'i1', label: 'Score', expression: 'score', type: 'number' };
const minimum: DecisionInputColumn = { id: 'i2', label: 'Minimum', expression: 'minimum', type: 'number' };

describe('the hint under the grid', () => {
  it('shows comparing with a column the table has', () => {
    expect(columnComparisonHint([score, minimum])).toEqual({ example: '> minimum', meaning: 'more than Minimum' });
  });

  it('compares a column that is not a number for sameness', () => {
    const tier: DecisionInputColumn = { id: 'i3', label: 'Tier', expression: 'tier', type: 'string' };
    expect(columnComparisonHint([score, tier])).toEqual({ example: '= tier', meaning: 'the same as Tier' });
  });

  it('shows the notation alone when there is no other column yet', () => {
    expect(columnComparisonHint([score])).toEqual({ example: '> minimum' });
  });
});
