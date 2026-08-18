import { describe, expect, it } from 'bun:test';

import { byUrgency, countUrgent, HIGH_PRIORITY, urgencyOf, type UrgencyInput } from './taskUrgency';

const now = new Date('2026-08-18T12:00:00Z');

function at(offsetHours: number): string {
  return new Date(now.getTime() + offsetHours * 3_600_000).toISOString();
}

/**
 * The inbox is where somebody spends their working day, and the question they
 * ask it is always the same: what should I do next. A list sorted by creation
 * date does not answer that, and "Priority: 70" answers it only for whoever
 * remembers what 70 means.
 */
describe('urgencyOf', () => {
  it('calls a late task late, and says how late', () => {
    const urgency = urgencyOf({ dueDate: at(-72) }, now);
    expect(urgency.level).toBe('overdue');
    expect(urgency.color).toBe('red');
    expect(urgency.label).toBe('Overdue by 3 days');
  });

  it('distinguishes today from soon from neither', () => {
    expect(urgencyOf({ dueDate: at(3) }, now).level).toBe('today');
    expect(urgencyOf({ dueDate: at(20) }, now).level).toBe('soon');
    expect(urgencyOf({ dueDate: at(20 * 24) }, now).level).toBe('normal');
  });

  it('treats a high priority as urgent even with no due date', () => {
    expect(urgencyOf({ priority: HIGH_PRIORITY }, now).level).toBe('important');
    expect(urgencyOf({ priority: HIGH_PRIORITY - 1 }, now).level).toBe('normal');
  });

  it('says nothing about a finished task', () => {
    expect(urgencyOf({ status: 'completed', dueDate: at(-500) }, now).level).toBe('done');
  });

  it('survives a due date nobody can parse', () => {
    const urgency = urgencyOf({ dueDate: 'next tuesday' }, now);
    expect(urgency.level).toBe('normal');
  });

  it('speaks in units a person uses', () => {
    expect(urgencyOf({ dueDate: at(-0.25) }, now).label).toBe('Overdue by 15 minutes');
    expect(urgencyOf({ dueDate: at(-5) }, now).label).toBe('Overdue by 5 hours');
    expect(urgencyOf({ dueDate: at(-24 * 9) }, now).label).toBe('Overdue by 9 days');
  });
});

/**
 * Ordering is the whole point: the top of the list has to be the thing to do
 * next, or the list is just a database table with rounded corners.
 */
describe('byUrgency', () => {
  it('puts late before due-today before soon before merely important', () => {
    const tasks: Array<UrgencyInput & { id: string }> = [
      { id: 'normal', priority: 1 },
      { id: 'important', priority: 90 },
      { id: 'soon', dueDate: at(20) },
      { id: 'today', dueDate: at(2) },
      { id: 'overdue', dueDate: at(-2) },
    ];

    expect([...tasks].sort(byUrgency(now)).map((t) => t.id)).toEqual([
      'overdue',
      'today',
      'soon',
      'important',
      'normal',
    ]);
  });

  it('sorts the later of two overdue tasks first', () => {
    const tasks = [
      { id: 'an hour', dueDate: at(-1) },
      { id: 'a week', dueDate: at(-24 * 7) },
    ];
    expect([...tasks].sort(byUrgency(now)).map((t) => t.id)).toEqual(['a week', 'an hour']);
  });

  it('breaks a tie with the older task, because somebody has been waiting longer', () => {
    const tasks = [
      { id: 'new', priority: 90, createdAt: at(-1) },
      { id: 'old', priority: 90, createdAt: at(-100) },
    ];
    expect([...tasks].sort(byUrgency(now)).map((t) => t.id)).toEqual(['old', 'new']);
  });

  it('sinks finished work', () => {
    const tasks = [
      { id: 'done', status: 'completed', priority: 99 },
      { id: 'open', priority: 1 },
    ];
    expect([...tasks].sort(byUrgency(now)).map((t) => t.id)).toEqual(['open', 'done']);
  });

  /**
   * A list rendered in one pass must rate every task against the same instant.
   * A boundary crossing mid-render would sort two identical tasks differently,
   * which shows up as a list that shuffles while you look at it.
   */
  it('is stable when every task is rated against the same instant', () => {
    const tasks = Array.from({ length: 50 }, (_, i) => ({ id: `t${i}`, dueDate: at(-i) }));
    const first = [...tasks].sort(byUrgency(now)).map((t) => t.id);
    for (let i = 0; i < 5; i += 1) {
      expect([...tasks].sort(byUrgency(now)).map((t) => t.id)).toEqual(first);
    }
  });
});

describe('countUrgent', () => {
  it('counts what is late and what is nearly late', () => {
    const counts = countUrgent(
      [{ dueDate: at(-1) }, { dueDate: at(-50) }, { dueDate: at(3) }, { dueDate: at(200) }, { priority: 90 }],
      now,
    );
    expect(counts).toEqual({ overdue: 2, dueToday: 1 });
  });

  it('does not count finished work as late', () => {
    expect(countUrgent([{ dueDate: at(-100), status: 'completed' }], now)).toEqual({ overdue: 0, dueToday: 0 });
  });
});
