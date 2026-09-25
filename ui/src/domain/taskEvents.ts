/**
 * The events after which a list of tasks may be out of date.
 *
 * A withdrawn task is one of them. The inbox listened for created, claimed,
 * updated and completed only, so a task the engine took away — its boundary
 * event fired, or a migration ended it — stayed on screen until somebody
 * reloaded, and acting on it failed.
 */
export const TASK_LIST_EVENTS = [
  'TaskCreated',
  'TaskClaimed',
  'TaskUpdated',
  'TaskCompleted',
  'TaskCanceled',
] as const;
