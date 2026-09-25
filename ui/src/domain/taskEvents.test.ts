import { describe, expect, it } from 'bun:test';

import { TASK_LIST_EVENTS } from './taskEvents';

describe('TASK_LIST_EVENTS', () => {
  it('includes a withdrawn task, which the inbox did not listen for', () => {
    expect(TASK_LIST_EVENTS).toContain('TaskCanceled');
  });

  it('covers every task event the engine raises', () => {
    // server/domains/entities/process_event.go
    for (const type of ['TaskCreated', 'TaskClaimed', 'TaskUpdated', 'TaskCompleted', 'TaskCanceled']) {
      expect(TASK_LIST_EVENTS).toContain(type);
    }
  });
});
