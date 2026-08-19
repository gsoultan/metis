/**
 * Doing one thing to many rows, and saying honestly what happened.
 *
 * The inbox's bulk claim was a `forEach` over `mutate`: forty selected tasks
 * became forty concurrent requests, forty cache invalidations and forty toasts,
 * and the selection was cleared before any of them answered. Three things go
 * wrong with that, and all three are worse the more rows somebody selects —
 * which is exactly when they reach for bulk.
 *
 * 1. **Nobody is told what failed.** Claiming is a race the engine allows: two
 *    people can select the same task and one of them loses. That is not an
 *    error to hide, it is the thing the person needs to know.
 * 2. **The selection is gone.** Having lost five of forty, there is no way to
 *    retry those five short of finding them again by eye.
 * 3. **It is a thundering herd.** This server has a backpressure interceptor
 *    precisely because unbounded concurrency is a problem; the UI should not be
 *    the thing generating it.
 *
 * So a bulk action runs with a bounded number in flight, waits for all of them,
 * and returns what happened per row. What the caller does with a partial
 * failure — keep those rows selected, say how many, offer a retry — is then a
 * decision it can actually make.
 */

/** What happened to one row. */
export interface BulkOutcome<Id> {
  id: Id;
  ok: boolean;
  /** Why it failed, in the words the server used. Absent when it succeeded. */
  reason?: string;
}

export interface BulkResult<Id> {
  succeeded: Id[];
  failed: BulkOutcome<Id>[];
  /** Every outcome in the order the ids were given, for a detailed report. */
  outcomes: BulkOutcome<Id>[];
}

/**
 * How many requests to have in flight at once.
 *
 * Six is the number of connections a browser opens per origin over HTTP/1.1, so
 * a seventh would queue in the browser rather than reach the server sooner. It
 * is low enough that a bulk action on a few hundred rows does not look like an
 * attack to the rate limiter, and high enough that it is plainly faster than
 * one at a time.
 */
export const DEFAULT_BULK_CONCURRENCY = 6;

export interface BulkOptions {
  concurrency?: number;
  /** Called after each row settles, for a progress indicator. */
  onProgress?: (done: number, total: number) => void;
}

/**
 * Applies `act` to every id, at most `concurrency` at a time.
 *
 * Never rejects: a row that throws is a failed row, not a failed bulk action.
 * Stopping at the first failure would leave the person with no idea which of
 * the rest had been done.
 */
export async function runBulk<Id>(
  ids: readonly Id[],
  act: (id: Id) => Promise<unknown>,
  options: BulkOptions = {},
): Promise<BulkResult<Id>> {
  const limit = Math.max(1, options.concurrency ?? DEFAULT_BULK_CONCURRENCY);
  const outcomes: BulkOutcome<Id>[] = new Array(ids.length);
  let next = 0;
  let done = 0;

  const worker = async (): Promise<void> => {
    for (;;) {
      const index = next++;
      if (index >= ids.length) return;
      const id = ids[index];
      try {
        await act(id);
        outcomes[index] = { id, ok: true };
      } catch (error: unknown) {
        outcomes[index] = { id, ok: false, reason: describe(error) };
      }
      done += 1;
      options.onProgress?.(done, ids.length);
    }
  };

  await Promise.all(Array.from({ length: Math.min(limit, ids.length) }, worker));

  return {
    succeeded: outcomes.filter((o) => o.ok).map((o) => o.id),
    failed: outcomes.filter((o) => !o.ok),
    outcomes,
  };
}

/**
 * One sentence for the notification.
 *
 * "12 tasks claimed" when it all worked. When it did not, the count that
 * failed comes first, because that is the part that needs somebody to do
 * something — and the single most common reason is named, since forty rows
 * failing for one reason is one problem, not forty.
 */
export function summarise<Id>(result: BulkResult<Id>, noun: string, verb: string): string {
  const { succeeded, failed } = result;
  const plural = (n: number) => (n === 1 ? noun : `${noun}s`);

  if (failed.length === 0) {
    return `${succeeded.length} ${plural(succeeded.length)} ${verb}`;
  }
  if (succeeded.length === 0) {
    return `None of the ${failed.length} ${plural(failed.length)} could be ${verb}: ${commonReason(failed)}`;
  }
  return `${succeeded.length} ${verb}, ${failed.length} could not be: ${commonReason(failed)}`;
}

/** The reason that accounts for the most failures, so one cause reads as one cause. */
function commonReason<Id>(failed: BulkOutcome<Id>[]): string {
  const counts = new Map<string, number>();
  for (const outcome of failed) {
    const reason = outcome.reason ?? 'no reason given';
    counts.set(reason, (counts.get(reason) ?? 0) + 1);
  }
  let best = '';
  let bestCount = 0;
  for (const [reason, count] of counts) {
    if (count > bestCount) {
      best = reason;
      bestCount = count;
    }
  }
  return counts.size > 1 ? `${best} (and ${counts.size - 1} other reason${counts.size === 2 ? '' : 's'})` : best;
}

function describe(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === 'string' && error) return error;
  return 'no reason given';
}
