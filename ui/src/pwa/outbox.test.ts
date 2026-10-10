import { afterEach, describe, expect, it } from 'bun:test';

import type { OutboxEntry } from '../domain/outbox';
import { flushOutbox, type OutboxQueue } from './outbox';

type GlobalWithStorage = typeof globalThis & { localStorage?: Pick<Storage, 'getItem'> };

function entry(over: Partial<OutboxEntry>): OutboxEntry {
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

function memoryQueue(entries: OutboxEntry[]): OutboxQueue & { entries: Map<string, OutboxEntry> } {
  const kept = new Map(entries.map((e) => [e.key, e]));
  return {
    entries: kept,
    available: () => true,
    list: async () => [...kept.values()],
    put: async (e) => { kept.set(e.key, e); },
    remove: async (key) => { kept.delete(key); },
  };
}

const originalFetch = globalThis.fetch;
const originalStorage = (globalThis as GlobalWithStorage).localStorage;

/** Signs `userId` in, and answers every request with `status`. Returns what was sent. */
function signedInAs(userId: string | null, status: number): string[] {
  const sentAuth: string[] = [];
  (globalThis as GlobalWithStorage).localStorage = {
    getItem: () => JSON.stringify({ state: userId ? { token: `token-of-${userId}`, user: { id: userId } } : {} }),
  };
  globalThis.fetch = (async (_input, init) => {
    sentAuth.push(String((init?.headers as Record<string, string>).Authorization));
    return new Response('{}', { status, headers: { 'Content-Type': 'application/json' } });
  }) as typeof fetch;
  return sentAuth;
}

describe('flushing the offline queue', () => {
  afterEach(() => {
    globalThis.fetch = originalFetch;
    (globalThis as GlobalWithStorage).localStorage = originalStorage;
  });

  it('sends nothing somebody else queued on this device, and leaves it queued', async () => {
    const queue = memoryQueue([entry({ key: 'theirs', userId: 'ana' })]);
    const sent = signedInAs('ben', 200);

    expect(await flushOutbox(queue)).toEqual([]);

    expect(sent).toEqual([]);
    expect(queue.entries.has('theirs')).toBe(true);
  });

  it('sends the signed-in person’s own work under their session', async () => {
    const queue = memoryQueue([
      entry({ key: 'theirs', userId: 'ana', queuedAt: '2026-09-07T09:00:00.000Z' }),
      entry({ key: 'mine', userId: 'ben' }),
    ]);
    const sent = signedInAs('ben', 200);

    const results = await flushOutbox(queue);

    expect(results.map((r) => r.entry.key)).toEqual(['mine']);
    expect(sent).toEqual(['Bearer token-of-ben']);
    expect([...queue.entries.keys()]).toEqual(['theirs']);
  });

  it('does not count an expired session as an attempt', async () => {
    const queue = memoryQueue([entry({ key: 'mine', userId: 'ben', attempts: 4 })]);
    signedInAs('ben', 401);

    for (let i = 0; i < 3; i++) await flushOutbox(queue);

    expect(queue.entries.get('mine')?.attempts).toBe(4);
  });

  it('sends nothing while nobody is signed in', async () => {
    const queue = memoryQueue([entry({ key: 'mine', userId: 'ben' })]);
    const sent = signedInAs(null, 200);

    expect(await flushOutbox(queue)).toEqual([]);
    expect(sent).toEqual([]);
  });
});
