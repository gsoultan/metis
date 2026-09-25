/**
 * The figures on the dashboard's tiles, from the project's statistics.
 */

/** The counts the task tile is built from, as the statistics call reports them. */
export interface TaskCounts {
  totalTasks?: number;
  pendingTasks?: number;
  completedTasks?: number;
}

export interface TaskCompletion {
  done: number;
  total: number;
  /** Whole percent; 0 when there are no tasks. */
  rate: number;
}

/**
 * How much of the project's work is finished.
 *
 * Counted from completed tasks. It was total minus unclaimed, so a task
 * somebody had only claimed — and a task the engine had withdrawn — read as
 * done: three tasks with one finished showed 67%.
 */
export function taskCompletion(counts: TaskCounts): TaskCompletion {
  const total = counts.totalTasks ?? 0;
  const done = counts.completedTasks ?? 0;
  return { done, total, rate: total > 0 ? Math.round((done / total) * 100) : 0 };
}
