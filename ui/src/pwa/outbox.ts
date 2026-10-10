/**
 * Sending the work that was done offline.
 *
 * Queuing is the easy half. This is the half where a queued approval meets a
 * server that may have moved on: the task claimed by somebody else, the
 * instance cancelled, the session expired. Every one of those is an answer
 * somebody has to see, so nothing is dropped silently and nothing is retried
 * forever.
 */

import { v7 as uuidv7 } from 'uuid';

import {
  afterAttempt,
  classifyNetworkFailure,
  classifyResponse,
  isExhausted,
  ownedBy,
  type FlushOutcome,
  type OutboxEntry,
} from '../domain/outbox';
import { API_BASE_URL } from '../services/shared/config';
import { getAuthToken, getAuthUserId } from '../services/shared/auth';
import { reportSessionRefused } from '../services/shared/sessionRefusal';
import { listOutbox, outboxAvailable, putOutbox, removeOutbox } from './outboxStore';

/** Where the queue is kept: IndexedDB, or a stand-in in a test. */
export interface OutboxQueue {
  available: () => boolean;
  list: () => Promise<OutboxEntry[]>;
  put: (entry: OutboxEntry) => Promise<void>;
  remove: (key: string) => Promise<void>;
}

const browserQueue: OutboxQueue = {
  available: outboxAvailable,
  list: listOutbox,
  put: putOutbox,
  remove: removeOutbox,
};

/** What a caller must say to queue something. */
export interface QueueableRequest {
  method: string;
  path: string;
  body?: unknown;
  /** What this is, in words, for the pending list and any later refusal. */
  label: string;
}

/**
 * Records an action to be sent later.
 *
 * The idempotency key is generated **here**, when the person acted, not when
 * the request is eventually sent. That is the whole safety property: a flush
 * that succeeds on the server but loses the answer on the way back is retried
 * with the same key, and the server replays its original response instead of
 * completing the task a second time.
 */
export async function queueRequest(request: QueueableRequest): Promise<OutboxEntry> {
  const entry: OutboxEntry = {
    key: uuidv7(),
    method: request.method,
    path: request.path,
    body: request.body,
    label: request.label,
    queuedAt: new Date().toISOString(),
    attempts: 0,
    userId: getAuthUserId() ?? undefined,
  };
  await putOutbox(entry);
  void requestBackgroundSync();
  return entry;
}

/** What happened to one entry, for reporting back to the person. */
export interface FlushResult {
  entry: OutboxEntry;
  outcome: FlushOutcome;
}

/**
 * Tries to send everything waiting, oldest first.
 *
 * Sequential on purpose. These are steps in somebody's day — claim then
 * complete — and sending them at once would let the second arrive before the
 * first. It also keeps a flush over a weak connection from opening a dozen
 * sockets at once.
 */
export async function flushOutbox(queue: OutboxQueue = browserQueue): Promise<FlushResult[]> {
  if (!queue.available()) return [];

  // Read once, so every entry in this flush goes out under the session that
  // was checked against its owner.
  const token = getAuthToken();
  const userId = getAuthUserId();
  if (!token || !userId) return [];

  const results: FlushResult[] = [];
  // Only this person's work: an approval somebody else queued on this device
  // waits for them, rather than going out in this person's name.
  for (const entry of ownedBy(await queue.list(), userId)) {
    // An entry that has run out of attempts stays put and stays counted. It
    // needs a person, and quietly deleting it would lose an approval they
    // believe they made.
    if (isExhausted(entry)) continue;

    const outcome = await send(entry, token);
    results.push({ entry, outcome });

    const kept = afterAttempt(entry, outcome);
    if (kept === null) {
      await queue.remove(entry.key);
      continue;
    }

    if (kept !== entry) await queue.put(kept);
    // Stop at the first thing that could not be sent: the rest are almost
    // certainly blocked by the same cause, and hammering a dead connection
    // burns everything's attempts at once.
    break;
  }
  return results;
}

async function send(entry: OutboxEntry, token: string): Promise<FlushOutcome> {
  try {
    const response = await fetch(`${API_BASE_URL}${entry.path}`, {
      method: entry.method,
      headers: {
        'Content-Type': 'application/json',
        // The server keys on this alongside the method, path, tenant and
        // caller, so a replay returns the original answer.
        'Idempotency-Key': entry.key,
        Authorization: `Bearer ${token}`,
      },
      body: entry.body === undefined ? undefined : JSON.stringify(entry.body),
    });

    if (response.ok) return classifyResponse(response.status);
    if (response.status === 401) reportSessionRefused(token);

    const data = (await response.json().catch(() => ({}))) as { error?: unknown };
    const message = typeof data.error === 'string' ? data.error : undefined;
    return classifyResponse(response.status, message);
  } catch (error) {
    return classifyNetworkFailure(error);
  }
}

/**
 * Asks the browser to flush the queue when the connection returns, even with no
 * tab open.
 *
 * Background Sync is not everywhere — Safari has none — so it is a bonus rather
 * than the mechanism. The reliable path is the `online` listener and the flush
 * on load, both of which need the app to be open; this simply does better when
 * the browser allows it.
 */
export async function requestBackgroundSync(): Promise<void> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;
  try {
    const registration = (await navigator.serviceWorker.ready) as ServiceWorkerRegistration & {
      sync?: { register: (tag: string) => Promise<void> };
    };
    await registration.sync?.register('metis-outbox');
  } catch {
    // No Background Sync, or permission refused. The foreground flush covers it.
  }
}
