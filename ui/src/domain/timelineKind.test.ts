import { describe, expect, it } from 'bun:test';

import { timelineKind } from './timelineKind';

describe('timelineKind', () => {
  it('reads a task action the same whichever writer spelled it', () => {
    // Entries written before task actions were audited once keep the
    // observer's spelling; the ones written since have the service's.
    expect(timelineKind('TaskClaimed')).toBe(timelineKind('task_claimed'));
    expect(timelineKind('TaskCompleted')).toBe(timelineKind('task_completed'));
    expect(timelineKind('TaskCreated')).toBe(timelineKind('task_created'));
  });

  it('tells the task actions apart that the observer could not', () => {
    // The observer recorded a release and a delegation both as "TaskUpdated",
    // and an assignment as a claim.
    expect(timelineKind('task_unclaimed')).toBe('released');
    expect(timelineKind('task_delegated')).toBe('delegated');
    expect(timelineKind('task_assigned')).toBe('assigned');
  });

  it('treats anything it does not know, including object keys, as other', () => {
    expect(timelineKind('TaskUpdated')).toBe('other');
    expect(timelineKind('constructor')).toBe('other');
    expect(timelineKind('')).toBe('other');
  });
});
