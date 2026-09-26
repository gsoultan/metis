import { describe, expect, it } from 'bun:test';

import { advancedVisibility } from './disclosure';
import { loopSummary } from './loopSummary';

describe('loopSummary', () => {
  it.each([
    ['no type', {}],
    ['an empty type', { multiInstanceType: '' }],
    ['"none"', { multiInstanceType: 'none', collection: 'orders' }],
  ])('says nothing for a step with %s, which runs once', (_name, data) => {
    expect(loopSummary(data)).toBeUndefined();
  });

  it('says what a parallel loop goes through', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders' }))
      .toBe('Runs once for each item in orders, all at the same time.');
  });

  it('says what a sequential loop goes through', () => {
    expect(loopSummary({ multiInstanceType: 'sequential', collection: 'orders' }))
      .toBe('Runs once for each item in orders, one after another.');
  });

  it('counts, when there is no list', () => {
    expect(loopSummary({ multiInstanceType: 'sequential', loopCardinality: 3 }))
      .toBe('Runs 3 times, one after another.');
    expect(loopSummary({ multiInstanceType: 'parallel', loopCardinality: 1 }))
      .toBe('Runs once, all at the same time.');
  });

  it('goes by the list when both are set, as the engine does', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders', loopCardinality: 3 }))
      .toBe('Runs once for each item in orders, all at the same time.');
  });

  it('names the item each run sees, when there is a list', () => {
    expect(loopSummary({ multiInstanceType: 'parallel', collection: 'orders', elementVariable: 'order' }))
      .toBe('Runs once for each item in orders, all at the same time. Each run sees its item as order.');
    // With only a count there is no item to name.
    expect(loopSummary({ multiInstanceType: 'parallel', loopCardinality: 2, elementVariable: 'order' }))
      .toBe('Runs 2 times, all at the same time.');
  });

  it('says what ends it, when a condition does', () => {
    expect(loopSummary({
      multiInstanceType: 'parallel',
      collection: 'bids',
      completionCondition: 'nrOfCompletedInstances >= 2',
    })).toBe('Runs once for each item in bids, all at the same time. Moves on once this is true: nrOfCompletedInstances >= 2.');
  });

  it('says so when a loop names nothing to go through', () => {
    // The engine finds nothing to repeat over and runs the step once.
    expect(loopSummary({ multiInstanceType: 'parallel' }))
      .toBe('Set to repeat, but it names no list and no count, so it runs once.');
  });

  const CANNOT_RUN = (type: string) =>
    `Set to repeat as "${type}", a kind of repeat that cannot run. ` +
    'Deploying this process will be refused, and a version already deployed with it fails to start.';

  it.each(['loop', 'constructor', 'Parallel'])('says a step set to repeat as %s cannot run, rather than hiding it', (type) => {
    // The engine runs a loop in parallel or in sequence. It refuses any other
    // kind at deploy, and fails a version stored before it did when started.
    expect(loopSummary({ multiInstanceType: type, collection: 'orders' })).toBe(CANNOT_RUN(type));
  });

  it('says so even when it names nothing to go through', () => {
    // The engine refuses the kind before it looks for a list or a count, so
    // such a step does not run once: it does not run.
    expect(loopSummary({ multiInstanceType: 'standard' })).toBe(CANNOT_RUN('standard'));
  });

  it('is what decides whether basic mode shows the loop', () => {
    expect(advancedVisibility(false, loopSummary({ multiInstanceType: 'none' }))).toBe('hidden');
    expect(advancedVisibility(false, loopSummary({ multiInstanceType: 'parallel', collection: 'orders' }))).toBe('summary');
    expect(advancedVisibility(true, loopSummary({}))).toBe('edit');
  });
});
