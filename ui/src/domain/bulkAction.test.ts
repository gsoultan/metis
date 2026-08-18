import { describe, expect, test } from 'bun:test';

import { DEFAULT_BULK_CONCURRENCY, runBulk, summarise } from './bulkAction';

describe('runBulk', () => {
  test('reports each row separately rather than all-or-nothing', async () => {
    const result = await runBulk(['a', 'b', 'c'], async (id) => {
      if (id === 'b') throw new Error('Task already claimed by someone else');
    });

    expect(result.succeeded).toEqual(['a', 'c']);
    expect(result.failed).toEqual([{ id: 'b', ok: false, reason: 'Task already claimed by someone else' }]);
  });

  test('carries on after a failure instead of stopping at the first one', async () => {
    const attempted: number[] = [];
    const result = await runBulk([1, 2, 3, 4], async (id) => {
      attempted.push(id);
      throw new Error('nope');
    });

    expect(attempted.sort()).toEqual([1, 2, 3, 4]);
    expect(result.failed).toHaveLength(4);
    expect(result.succeeded).toEqual([]);
  });

  test('holds the concurrency limit', async () => {
    let inFlight = 0;
    let peak = 0;

    await runBulk(Array.from({ length: 50 }, (_, i) => i), async () => {
      inFlight += 1;
      peak = Math.max(peak, inFlight);
      await Promise.resolve();
      await Promise.resolve();
      inFlight -= 1;
    });

    expect(peak).toBeLessThanOrEqual(DEFAULT_BULK_CONCURRENCY);
    // And it is genuinely concurrent, not serialised one at a time — that was
    // the other way to "fix" the thundering herd, and it makes bulk useless.
    expect(peak).toBeGreaterThan(1);
  });

  test('starts no more workers than there are rows', async () => {
    let peak = 0;
    let inFlight = 0;
    await runBulk(['only-one'], async () => {
      inFlight += 1;
      peak = Math.max(peak, inFlight);
      await Promise.resolve();
      inFlight -= 1;
    });
    expect(peak).toBe(1);
  });

  test('does every row exactly once', async () => {
    const seen: number[] = [];
    const ids = Array.from({ length: 37 }, (_, i) => i);
    await runBulk(ids, async (id) => {
      seen.push(id);
    });
    expect(seen.sort((a, b) => a - b)).toEqual(ids);
  });

  test('an empty selection is not an error', async () => {
    const result = await runBulk([], async () => {
      throw new Error('should never be called');
    });
    expect(result).toEqual({ succeeded: [], failed: [], outcomes: [] });
  });

  test('reports progress as rows settle, successes and failures alike', async () => {
    const progress: number[] = [];
    await runBulk([1, 2, 3], async (id) => {
      if (id === 2) throw new Error('nope');
    }, { onProgress: (done) => progress.push(done) });

    expect(progress.sort()).toEqual([1, 2, 3]);
  });

  test('describes a thrown non-Error without saying "undefined"', async () => {
    const result = await runBulk(['x'], async () => {
      throw 'the server hung up';
    });
    expect(result.failed[0].reason).toBe('the server hung up');

    const opaque = await runBulk(['y'], async () => {
      throw { weird: true };
    });
    expect(opaque.failed[0].reason).toBe('no reason given');
  });
});

describe('summarise', () => {
  const ok = (ids: string[]) => ({ succeeded: ids, failed: [], outcomes: [] });

  test('says how many when it all worked', () => {
    expect(summarise(ok(['a', 'b', 'c']), 'task', 'claimed')).toBe('3 tasks claimed');
    expect(summarise(ok(['a']), 'task', 'claimed')).toBe('1 task claimed');
  });

  test('leads with the failures, because those are what need doing', () => {
    const result = {
      succeeded: ['a', 'b'],
      failed: [{ id: 'c', ok: false, reason: 'Already claimed' }],
      outcomes: [],
    };
    expect(summarise(result, 'task', 'claimed')).toBe('2 claimed, 1 could not be: Already claimed');
  });

  test('reads one cause as one cause, not forty', () => {
    const failed = Array.from({ length: 40 }, (_, i) => ({
      id: String(i),
      ok: false,
      reason: 'Service unavailable',
    }));
    expect(summarise({ succeeded: [], failed, outcomes: [] }, 'task', 'claimed')).toBe(
      'None of the 40 tasks could be claimed: Service unavailable',
    );
  });

  test('names the commonest reason and admits there were others', () => {
    const failed = [
      { id: '1', ok: false, reason: 'Already claimed' },
      { id: '2', ok: false, reason: 'Already claimed' },
      { id: '3', ok: false, reason: 'Not permitted' },
    ];
    expect(summarise({ succeeded: [], failed, outcomes: [] }, 'task', 'claimed')).toBe(
      'None of the 3 tasks could be claimed: Already claimed (and 1 other reason)',
    );
  });
});
