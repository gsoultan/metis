import { describe, expect, it } from 'bun:test';

import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { format, type Values } from '../i18n/translate';
import { describeHandOver } from './handOverNarrative';

const english = (key: string, values?: Values) => format(en, key, values);
const indonesian = (key: string, values?: Values) => format(id, key, values);
const TASK = 'Approve the refund';

/*
 * The trail used to name the person a task went to as the one who acted, and
 * nobody for a release. It now records the caller, who had the task, who has
 * it and why, and the timeline says so in a sentence.
 */
describe('a hand-over on the timeline', () => {
  it.each([
    ['task_assigned', { actor: 'ana', target: 'citra' }, 'ana assigned "Approve the refund" to citra'],
    ['task_assigned', { actor: 'ana', target: 'citra', previous_holder: 'budi', reason: 'budi is on leave' },
      'ana reassigned "Approve the refund" from budi to citra: budi is on leave'],
    ['task_assigned', { actor: 'ana', target: 'citra', previous_holder: 'budi', candidate_override: true, reason: 'nobody in finance is in' },
      'ana reassigned "Approve the refund" from budi to citra, who is not one of the people it is offered to: nobody in finance is in'],
    ['task_delegated', { actor: 'budi', target: 'citra', previous_holder: 'budi', owner: 'budi' }, 'budi delegated "Approve the refund" to citra'],
    ['task_delegated', { actor: 'ana', target: 'citra', previous_holder: 'budi', owner: 'budi', reason: 'budi asked by phone' },
      'ana delegated "Approve the refund" from budi to citra: budi asked by phone'],
    ['task_resolved', { actor: 'citra', target: 'budi', previous_holder: 'citra' }, 'citra handed "Approve the refund" back to budi'],
    ['task_resolved', { actor: 'ana', target: 'budi', previous_holder: 'citra', reason: 'citra is away' },
      'ana handed "Approve the refund" back from citra to budi: citra is away'],
    ['task_unclaimed', { actor: 'budi', previous_holder: 'budi' }, 'budi released "Approve the refund" back to the queue'],
    ['task_unclaimed', { actor: 'ana', previous_holder: 'budi', reason: 'budi left' },
      'ana released "Approve the refund" from budi back to the queue: budi left'],
    ['task_edited', { actor: 'budi', changes: { due_date: { before: null, after: '2026-12-01T09:00:00Z' } } },
      'budi changed the due date of "Approve the refund"'],
    ['task_edited', { actor: 'budi', changes: { priority: {}, name: {} } }, 'budi changed the name and the priority of "Approve the refund"'],
    ['task_edited', { actor: 'ana', changes: { name: {}, priority: {}, due_date: {} }, reason: 'the customer called' },
      'ana changed the name, the priority and the due date of "Approve the refund": the customer called'],
  ])('says %s %j as a sentence', (type, data, sentence) => {
    expect(describeHandOver(type as string, data as Record<string, unknown>, TASK, english)).toBe(sentence as string);
  });

  it('says it in the reader\u2019s language, leaving the reason as it was written', () => {
    expect(describeHandOver('task_assigned', { actor: 'ana', target: 'citra', previous_holder: 'budi', reason: 'budi is on leave' }, TASK, indonesian))
      .toBe('ana mengalihkan "Approve the refund" dari budi kepada citra: budi is on leave');
    expect(describeHandOver('task_edited', { actor: 'budi', changes: { name: {}, due_date: {} } }, TASK, indonesian))
      .toBe('budi mengubah nama dan tenggat pada "Approve the refund"');
  });

  it('keeps a reason as written, braces and markup included', () => {
    const reason = '{actor} <b>now</b>\nsecond line';
    expect(describeHandOver('task_unclaimed', { actor: 'ana', previous_holder: 'budi', reason }, TASK, english))
      .toBe(`ana released "Approve the refund" from budi back to the queue: ${reason}`);
  });

  /*
   * An entry written before the trail kept who acted has nothing to build a
   * sentence from, and neither has any other kind of entry: the timeline shows
   * what the server stored for those.
   */
  it.each([
    ['an old assignment, which names only who the task went to', 'task_assigned', { actor: 'citra' }],
    ['an old release, which names nobody', 'task_unclaimed', { actor: '' }],
    ['a claim', 'task_claimed', { actor: 'budi', target: 'budi' }],
    ['an edit that changed nothing it knows', 'task_edited', { actor: 'budi', changes: { colour: {} } }],
    ['an entry with no data', 'task_delegated', undefined],
    ['an object key', 'constructor', { actor: 'budi', target: 'citra' }],
  ])('has no sentence for %s', (_, type, data) => {
    expect(describeHandOver(type as string, data as Record<string, unknown> | undefined, TASK, english)).toBeNull();
  });

  it('has none without the task\u2019s name to put in it', () => {
    expect(describeHandOver('task_assigned', { actor: 'ana', target: 'citra' }, '', english)).toBeNull();
  });
});
