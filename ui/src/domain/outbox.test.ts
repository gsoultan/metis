import { describe, expect, it } from 'bun:test';

import {
  afterAttempt,
  classifyNetworkFailure,
  classifyResponse,
  describeQueue,
  inSendOrder,
  isExhausted,
  MAX_ATTEMPTS,
  ownedBy,
  type OutboxEntry,
} from './outbox';

function entry(over: Partial<OutboxEntry> = {}): OutboxEntry {
  return {
    key: 'k1',
    method: 'POST',
    path: '/tasks/t1/complete',
    body: {},
    label: 'Complete "Manager approves"',
    queuedAt: '2026-09-07T10:00:00.000Z',
    attempts: 0,
    ...over,
  };
}

/*
 * The dividing line is whether sending the same request again could produce a
 * different answer. Getting it wrong in either direction is bad: a queue that
 * never empties, or somebody's approval silently discarded.
 */
describe('reading the server’s answer', () => {
  it('treats success as done', () => {
    expect(classifyResponse(200).kind).toBe('sent');
    expect(classifyResponse(204).kind).toBe('sent');
  });

  it('keeps trying when the server might yet succeed', () => {
    for (const status of [500, 502, 503, 504, 429]) {
      expect(classifyResponse(status).kind).toBe('retry');
    }
  });

  it('keeps trying when the server asks for the same request again', () => {
    // The idempotency interceptor answers 408 while the original is still in
    // flight, and explicitly asks for a retry with the same key.
    expect(classifyResponse(408).kind).toBe('retry');
    expect(classifyResponse(425).kind).toBe('retry');
  });

  /*
   * A session can expire while work sits in the queue. Retrying with the same
   * expired token cannot help, but the work must survive until they sign in —
   * discarding it would lose an approval somebody already made.
   */
  it('keeps work when the session has expired, without counting it as an attempt', () => {
    expect(classifyResponse(401).kind).toBe('signedOut');
    expect(afterAttempt(entry({ attempts: 2 }), classifyResponse(401))).toEqual(entry({ attempts: 2 }));
    expect(classifyResponse(403).kind).toBe('retry');
    expect(afterAttempt(entry({ attempts: 2 }), classifyResponse(403))?.attempts).toBe(3);
  });

  it('sends and shows only the signed-in person’s work', () => {
    const queue = [entry({ key: 'a', userId: 'ana' }), entry({ key: 'b', userId: 'ben' }), entry({ key: 'c' })];
    expect(ownedBy(queue, 'ana').map((e) => e.key)).toEqual(['a']);
    // Nobody signed in, and an entry with no owner, are nobody's to send.
    expect(ownedBy(queue, null)).toEqual([]);
  });

  /*
   * The task was completed by somebody else, or reassigned, while this sat in
   * a pocket. Retrying will refuse identically forever.
   */
  it('gives up when the server understood and refused', () => {
    const outcome = classifyResponse(409, 'That task is already completed.');
    expect(outcome.kind).toBe('refused');
    expect(outcome.kind === 'refused' && outcome.reason).toBe('That task is already completed.');
    expect(classifyResponse(400).kind).toBe('refused');
    expect(classifyResponse(404).kind).toBe('refused');
  });

  it('says something even when the server did not', () => {
    const outcome = classifyResponse(418);
    expect(outcome.kind === 'refused' && outcome.reason).toContain('418');
  });

  it('always retries a request that got no answer at all', () => {
    // Nothing was decided, so nothing has been lost.
    expect(classifyNetworkFailure(new Error('Failed to fetch')).kind).toBe('retry');
    expect(classifyNetworkFailure(undefined).kind).toBe('retry');
  });
});

describe('not trying forever', () => {
  it('stops after a bounded number of attempts', () => {
    expect(isExhausted(entry({ attempts: MAX_ATTEMPTS - 1 }))).toBe(false);
    expect(isExhausted(entry({ attempts: MAX_ATTEMPTS }))).toBe(true);
  });
});

describe('the order work is sent in', () => {
  it('sends oldest first, because that is the order they were done', () => {
    const sorted = inSendOrder([
      entry({ key: 'c', queuedAt: '2026-09-07T12:00:00.000Z' }),
      entry({ key: 'a', queuedAt: '2026-09-07T10:00:00.000Z' }),
      entry({ key: 'b', queuedAt: '2026-09-07T11:00:00.000Z' }),
    ]);
    expect(sorted.map((e) => e.key)).toEqual(['a', 'b', 'c']);
  });

  it('does not disturb the caller’s array', () => {
    const original = [entry({ key: 'b', queuedAt: '2026-09-07T12:00:00.000Z' }), entry({ key: 'a' })];
    inSendOrder(original);
    expect(original[0].key).toBe('b');
  });
});

describe('what the person is told', () => {
  it('says nothing when there is nothing waiting', () => {
    expect(describeQueue([])).toBe('');
  });

  it('counts in words, and gets one right', () => {
    expect(describeQueue([entry()])).toBe('1 change waiting to be sent');
    expect(describeQueue([entry({ key: 'a' }), entry({ key: 'b' })])).toBe('2 changes waiting to be sent');
  });

  /*
   * A count on its own would keep reading "work on its way" for something that
   * is never going anywhere. The distinction is the whole point of saying it in
   * words.
   */
  it('distinguishes waiting from stuck', () => {
    expect(describeQueue([entry({ attempts: MAX_ATTEMPTS })])).toBe('1 change could not be sent');
    expect(
      describeQueue([entry({ key: 'a' }), entry({ key: 'b', attempts: MAX_ATTEMPTS })]),
    ).toBe('2 changes waiting, 1 could not be sent');
  });
});
