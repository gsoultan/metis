import { describe, expect, it } from 'bun:test';

import {
  definitionName,
  describeDuration,
  humanizeNodeId,
  instanceReference,
  isRunning,
  startedAtFromId,
  statusChips,
  totalAcrossStatuses,
} from './instanceList';

describe('definitionName', () => {
  it('prefers what the instance itself carries', () => {
    expect(definitionName({ id: 'i', definition: { id: 'd', name: 'Expense claim' } }, [])).toBe('Expense claim');
    expect(definitionName({ id: 'i', definition: { id: 'd', key: 'expense' } }, [])).toBe('expense');
  });

  it('resolves through the directory when the instance only has an id', () => {
    const directory = [{ id: 'd', key: 'expense', name: 'Expense claim' }];
    expect(definitionName({ id: 'i', definition: { id: 'd' } }, directory)).toBe('Expense claim');
  });

  it('falls back to a generic word rather than an id', () => {
    expect(definitionName({ id: 'i', definition: { id: 'missing' } }, [])).toBe('Process');
  });
});

describe('humanizeNodeId', () => {
  it('drops the designer prefix and splits the words', () => {
    expect(humanizeNodeId('Activity_ApproveExpense')).toBe('Approve Expense');
    expect(humanizeNodeId('approve-expense')).toBe('Approve expense');
  });

  it('keeps a generated id that has no words in it', () => {
    expect(humanizeNodeId('Task_1')).toBe('Task_1');
  });
});

describe('startedAtFromId', () => {
  // 0x018f_0000_0000 ms since the epoch = 2024-04-21T09:32:31.104Z: the first 48 bits of a v7 id.
  const v7 = '018f0000-0000-7abc-8def-0123456789ab';

  it('reads the creation time out of a v7 id', () => {
    expect(startedAtFromId(v7)?.toISOString()).toBe('2024-04-21T09:32:31.104Z');
  });

  it('refuses to invent a time for an id that is not v7', () => {
    // Same digits, version nibble 4: a random UUID carries no timestamp.
    expect(startedAtFromId('018f0000-0000-4abc-8def-0123456789ab')).toBeNull();
    expect(startedAtFromId('not-an-id')).toBeNull();
  });
});

describe('instanceReference', () => {
  it('takes the random tail, which differs between rows the head does not', () => {
    // The bug: every UUIDv7 created the same week shares its first 8 chars,
    // so the old `id.slice(0, 8)` subtitle read identically on every row.
    const a = '018f0000-0000-7abc-8def-0123456789ab';
    const b = '018f0000-0000-7abc-8def-0123456789cd';
    expect(a.slice(0, 8)).toBe(b.slice(0, 8));
    expect(instanceReference(a)).toBe('#6789AB');
    expect(instanceReference(b)).toBe('#6789CD');
  });
});

describe('statusChips', () => {
  it('puts what needs a person first and what is finished last', () => {
    const chips = statusChips([
      { status: 'completed', total: 4120 },
      { status: 'active', total: 37 },
      { status: 'failed', total: 12 },
      { status: 'suspended', total: 2 },
    ]);
    expect(chips.map((c) => c.status)).toEqual(['failed', 'suspended', 'active', 'completed']);
  });

  it('drops a state the project has none of', () => {
    // A chip reading "Paused 0" can only ever empty the table.
    const chips = statusChips([
      { status: 'active', total: 3 },
      { status: 'suspended', total: 0 },
    ]);
    expect(chips.map((c) => c.status)).toEqual(['active']);
  });

  it('sorts a state it has no opinion about after the ones it does', () => {
    // Written by an older version: reported, but never ahead of "failed".
    const chips = statusChips([
      { status: 'archived', total: 9 },
      { status: 'completed', total: 1 },
      { status: 'failed', total: 1 },
    ]);
    expect(chips.map((c) => c.status)).toEqual(['failed', 'completed', 'archived']);
  });

  it('adds up to the project total', () => {
    expect(totalAcrossStatuses([
      { status: 'failed', total: 12 },
      { status: 'completed', total: 30 },
    ])).toBe(42);
  });
});

describe('isRunning', () => {
  it('counts a failed instance as still going', () => {
    // It is not finished, it is waiting for somebody to retry it — and how long
    // it has been waiting is the number that should be growing on screen.
    expect(isRunning('failed')).toBe(true);
    expect(isRunning('suspended')).toBe(true);
    expect(isRunning('active')).toBe(true);
  });

  it('counts an instance that will not move again as settled', () => {
    expect(isRunning('completed')).toBe(false);
    expect(isRunning('terminated')).toBe(false);
    expect(isRunning('CANCELLED')).toBe(false);
  });
});

describe('describeDuration', () => {
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  it('uses at most two units, largest first', () => {
    expect(describeDuration(0, 2 * hour + 14 * minute + 6_000)).toBe('2h 14m');
    expect(describeDuration(0, 3 * day + 5 * hour)).toBe('3d 5h');
    expect(describeDuration(0, 7 * minute)).toBe('7m');
  });

  it('drops an empty smaller unit rather than printing a zero', () => {
    expect(describeDuration(0, 2 * hour)).toBe('2h');
    expect(describeDuration(0, 3 * day)).toBe('3d');
  });

  it('reads a sub-minute span as just now', () => {
    expect(describeDuration(0, 6_000)).toBe('just now');
  });

  it('treats a little clock skew as just now, not as the future', () => {
    // The start comes from a server-generated id and the end from the browser.
    expect(describeDuration(10_000, 0)).toBe('just now');
    expect(describeDuration(10 * minute, 0)).toBeNull();
  });
});
