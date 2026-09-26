import { describe, expect, it } from 'bun:test';

import { taskCompletion } from './dashboardFigures';

describe('taskCompletion', () => {
  it('counts a task as done when it is completed, not when somebody claimed it', () => {
    // Three tasks: one completed, one claimed and still open, one nobody has
    // picked up. The tile said 2 of 3, 67% — total minus unclaimed.
    expect(taskCompletion({ totalTasks: 3, pendingTasks: 1, completedTasks: 1 })).toEqual({
      done: 1,
      total: 3,
      rate: 33,
    });
  });

  it('reads nothing into an empty project', () => {
    expect(taskCompletion({})).toEqual({ done: 0, total: 0, rate: 0 });
  });
});
