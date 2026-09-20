import { describe, expect, it } from 'bun:test';

import { MAX_FACTS, taskFacts } from './taskFacts';

describe('composing the facts that belong together', () => {
  /*
   * The reason this exists. The list showed `AMOUNT: 1750` and `CURRENCY: GBP`
   * as two separate truncated badges and left the approver to do the join.
   */
  it('renders an amount and its currency as one money value', () => {
    const { facts } = taskFacts({ amount: 1750, currency: 'GBP' });
    expect(facts[0].value).toContain('1,750');
    expect(facts[0].value).not.toBe('1750');
    expect(facts.some((f) => f.label.toLowerCase().includes('currency'))).toBe(false);
  });

  it('keeps the number when the currency is not an ISO code', () => {
    const { facts } = taskFacts({ amount: 40, currency: 'POINTS' });
    expect(facts[0].value).toBe('POINTS 40');
  });

  it('still shows an amount that carries no currency', () => {
    const { facts } = taskFacts({ amount: 12 });
    expect(facts[0].value).toBe('12');
  });

  it('reads an amount supplied as a string', () => {
    const { facts } = taskFacts({ amount: '1750', currency: 'GBP' });
    expect(facts[0].value).toContain('1,750');
  });
});

describe('what a list cell will not show', () => {
  it('skips the variable already used as the row reference', () => {
    const { facts } = taskFacts({ description: 'Expense of GBP 1,750', approver: 'alice' });
    expect(facts.map((f) => f.label)).toEqual(['Approver']);
  });

  it('skips objects and arrays rather than printing [object Object]', () => {
    const { facts } = taskFacts({ payload: { a: 1 }, lines: [1, 2], approver: 'alice' });
    expect(facts.map((f) => f.label)).toEqual(['Approver']);
  });

  it('skips null and empty values instead of rendering a blank badge', () => {
    const { facts } = taskFacts({ approver: null, note: '   ', level: 'director' });
    expect(facts.map((f) => f.label)).toEqual(['Level']);
  });

  it('bounds a long value rather than letting it set the column width', () => {
    const { facts } = taskFacts({ note: 'x'.repeat(200) });
    expect(facts[0].value.length).toBeLessThanOrEqual(28);
    expect(facts[0].value.endsWith('…')).toBe(true);
  });
});

describe('bounding the row', () => {
  it('shows no more than MAX_FACTS and counts the rest', () => {
    const { facts, hidden } = taskFacts({
      approver: 'finance', level: 'director', submittedby: 'alice',
      region: 'emea', costcentre: 'cc-1',
    });
    expect(facts).toHaveLength(MAX_FACTS);
    expect(hidden).toBe(2);
  });

  it('reports nothing hidden when everything fits', () => {
    expect(taskFacts({ approver: 'finance' }).hidden).toBe(0);
  });

  it('answers empty for a task with no variables at all', () => {
    expect(taskFacts(undefined)).toEqual({ facts: [], hidden: 0 });
    expect(taskFacts({})).toEqual({ facts: [], hidden: 0 });
  });
});

describe('the engine identifier never reaches the approver', () => {
  it('turns snake_case and camelCase keys into words', () => {
    const { facts } = taskFacts({ approval_level: 'director', submittedBy: 'alice' });
    expect(facts.map((f) => f.label)).toEqual(['Approval level', 'Submitted by']);
  });

  it('renders booleans as Yes and No rather than true and false', () => {
    const { facts } = taskFacts({ urgent: true, escalated: false });
    expect(facts.map((f) => f.value)).toEqual(['Yes', 'No']);
  });
});
