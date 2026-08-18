/**
 * How long a fetched answer stays believable.
 *
 * TanStack Query's default `staleTime` is zero, which means every query refetches
 * on every mount, every window focus and every reconnect. With twenty-seven
 * queries across this app that is a request storm on every navigation — click
 * from the inbox to the designer and back and the whole inbox is fetched again,
 * having not changed.
 *
 * The right value is not one number, because these resources do not change at
 * the same rate. A process definition changes when somebody deploys one; a task
 * list changes when anybody in the organisation does anything.
 *
 * None of these are load-bearing for correctness. The server pushes over SSE and
 * every mutation invalidates what it touched, so staleness here only decides how
 * long a *passive* view may lag before it refetches on its own.
 */

/** Definitions, decisions, connectors, webhooks — changed by a person, deliberately. */
export const AUTHORED_STALE_TIME = 5 * 60 * 1000;

/**
 * Tasks, instances, incidents — changed by the engine, constantly.
 *
 * Half a minute rather than zero: SSE and mutation invalidation already keep
 * these fresh when something happens, so this only governs a view nobody has
 * touched and nothing has announced.
 */
export const LIVE_STALE_TIME = 30 * 1000;

/** Organizations, projects, users, groups — the shape of the installation. */
export const DIRECTORY_STALE_TIME = 10 * 60 * 1000;

/**
 * How long an unused answer is kept before being thrown away.
 *
 * Longer than any staleTime, because the point of keeping it is that navigating
 * back shows something immediately while the refetch happens behind it. Throwing
 * it away at the same moment it goes stale would give a spinner on every return.
 */
export const DEFAULT_GC_TIME = 15 * 60 * 1000;

/**
 * The client-wide defaults.
 *
 * Retry is left at TanStack's default for queries and turned off for mutations:
 * a failed read is worth trying again, and a failed write may have happened.
 * Retrying "claim this task" that actually succeeded is how one person's task
 * ends up assigned to somebody else.
 */
export const queryClientDefaults = {
  queries: {
    staleTime: LIVE_STALE_TIME,
    gcTime: DEFAULT_GC_TIME,
    // A window regaining focus is not evidence that anything changed, and with
    // SSE connected it is never the way this app learns that something did.
    refetchOnWindowFocus: false,
    // A reconnect is different: while the connection was down, SSE was not
    // delivering, so there is a real gap to close.
    refetchOnReconnect: true,
  },
  mutations: {
    retry: 0,
  },
} as const;
