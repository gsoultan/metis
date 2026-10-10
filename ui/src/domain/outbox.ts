/**
 * Work done offline, waiting to be sent.
 *
 * The persona for the inbox is somebody approving work from a phone, often
 * somewhere with no signal. Until now a completion attempted offline simply
 * failed: the form was lost and the person was told the request did not go
 * through, which on a warehouse floor means doing it again later from memory.
 *
 * Queuing it is more than a cache, and this module holds the parts that make it
 * safe:
 *
 *   - **The idempotency key belongs to the action, not the attempt.** It is
 *     generated when somebody presses the button and stored with the entry, so
 *     a flush that partly succeeds and is retried replays rather than acts
 *     twice. The server keys on method, path, tenant and caller alongside it.
 *
 *   - **A queued write is a commitment against state that may have moved.** By
 *     the time it is sent the task may have been claimed, reassigned or
 *     cancelled. So a refusal is a result to show the person, never something
 *     to retry forever or discard quietly.
 *
 *   - **The queue is somebody's unsent work.** It has to be countable, and each
 *     entry has to be able to say what it was in words.
 */

/** What kind of answer the server gave, and what to do about it. */
export type FlushOutcome =
  /** Accepted. Drop the entry. */
  | { kind: 'sent' }
  /**
   * The server understood and refused: the task was already completed, or is
   * no longer theirs. Drop the entry and tell the person — retrying cannot fix
   * it, and keeping it means a queue that never empties.
   */
  | { kind: 'refused'; reason: string }
  /**
   * The request never got an answer, or the server failed. Keep the entry and
   * try again: this is the case the queue exists for.
   */
  | { kind: 'retry'; reason: string }
  /**
   * The session ended while the work was queued. Keep the entry, and do not
   * count this as an attempt: nothing about the work is wrong, and five
   * expired-token answers would otherwise mark it as needing a person before
   * they had even signed in again.
   */
  | { kind: 'signedOut'; reason: string };

export interface OutboxEntry {
  /** The idempotency key, and the entry's own identity. */
  key: string;
  method: string;
  path: string;
  body: unknown;
  /** What this was, in words, for the pending list. */
  label: string;
  /** ISO-8601, when the person did it. */
  queuedAt: string;
  /** How many times sending has been attempted. */
  attempts: number;
  /**
   * Who did it. The request is sent with whoever's token is in the tab at the
   * time, so an approval queued by one person must not go out when somebody
   * else has signed in on the same phone. Absent on an entry queued before
   * this was recorded; such an entry is never sent (see ownedBy).
   */
  userId?: string;
}

/**
 * How many times to try before giving up and asking the person.
 *
 * Not unlimited: an entry that can never be sent would otherwise sit in the
 * queue forever, and the count in the header would stop meaning "work on its
 * way" and start meaning "something is wrong and nobody said so".
 */
export const MAX_ATTEMPTS = 5;

/**
 * Reads an HTTP answer as an outcome.
 *
 * The dividing line is whether sending it again could produce a different
 * result. A 409 saying the task is already complete never will; a 503 might.
 */
export function classifyResponse(status: number, serverMessage?: string): FlushOutcome {
  if (status >= 200 && status < 300) {
    return { kind: 'sent' };
  }

  // 408 and 425 are the server asking for the same request again — the
  // idempotency interceptor answers 408 while the original is still running.
  if (status === 408 || status === 425 || status === 429 || status >= 500) {
    return { kind: 'retry', reason: serverMessage || `The server answered ${status}.` };
  }

  // 401 is a session that expired while the work was queued. Retrying with the
  // same expired token cannot help; the person has to sign in, and their work
  // is still here when they do.
  if (status === 401) {
    return { kind: 'signedOut', reason: serverMessage || 'You need to sign in again to send this.' };
  }
  if (status === 403) {
    return { kind: 'retry', reason: serverMessage || 'You need to sign in again to send this.' };
  }

  return { kind: 'refused', reason: serverMessage || `The server refused it (${status}).` };
}

/** A network failure is always worth retrying: nothing was decided. */
export function classifyNetworkFailure(error: unknown): FlushOutcome {
  const reason = error instanceof Error && error.message ? error.message : 'No connection.';
  return { kind: 'retry', reason };
}

/** Whether an entry has run out of attempts and needs a person to look. */
export function isExhausted(entry: OutboxEntry): boolean {
  return entry.attempts >= MAX_ATTEMPTS;
}

/**
 * The entries `userId` queued — the only ones sent or shown while they are
 * signed in. Everybody else's wait, untouched, for their own sign-in. An entry
 * with no recorded owner could be anybody's, so it is nobody's to send.
 */
export function ownedBy(entries: readonly OutboxEntry[], userId: string | null | undefined): OutboxEntry[] {
  if (!userId) return [];
  return entries.filter((entry) => entry.userId === userId);
}

/**
 * The entry as it should be kept after an attempt, or null to drop it.
 * Only an attempt the server could have answered differently counts.
 */
export function afterAttempt(entry: OutboxEntry, outcome: FlushOutcome): OutboxEntry | null {
  switch (outcome.kind) {
    case 'sent':
    case 'refused':
      return null;
    case 'signedOut':
      return entry;
    case 'retry':
      return { ...entry, attempts: entry.attempts + 1 };
  }
}

/** Oldest first: the order the person did them is the order they meant. */
export function inSendOrder(entries: readonly OutboxEntry[]): OutboxEntry[] {
  return [...entries].sort((a, b) => a.queuedAt.localeCompare(b.queuedAt));
}

/**
 * What the header says about the queue.
 *
 * Plain counting, in words, because "3" on its own beside an icon is not an
 * explanation of why an approval has not appeared on a colleague's screen.
 */
export function describeQueue(entries: readonly OutboxEntry[]): string {
  if (entries.length === 0) return '';
  const stuck = entries.filter(isExhausted).length;
  if (stuck > 0) {
    return stuck === entries.length
      ? `${plural(stuck, 'change')} could not be sent`
      : `${plural(entries.length, 'change')} waiting, ${stuck} could not be sent`;
  }
  return `${plural(entries.length, 'change')} waiting to be sent`;
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? '' : 's'}`;
}
