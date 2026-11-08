import { describe, expect, it } from 'bun:test';
import { nameCannotAct, whyNobodyCanAct } from './strandedTasks';

describe('a task nobody can act on', () => {
  it('names the holder who left or was deactivated', () => {
    expect(whyNobodyCanAct({ task_id: 't', name: 'Approve', kind: 'assignee_inactive', assignee: 'dewi', missing: [] }))
      .toBe('Held by dewi, who was deactivated');
    expect(whyNobodyCanAct({ task_id: 't', name: 'Approve', kind: 'assignee_gone', assignee: 'sari', missing: [] }))
      .toBe('Held by sari, who is no longer here');
  });

  it('lists every person and group it is offered to, with why each cannot act', () => {
    expect(whyNobodyCanAct({
      task_id: 't', name: 'Approve', kind: 'no_candidate', missing: [
        { name: 'bo', kind: 'person', why: 'inactive' },
        { name: 'night-shift', kind: 'group', why: 'empty' },
        { name: 'legal', kind: 'group', why: 'gone' },
      ],
    })).toBe('Nobody left to take it: bo was deactivated; group night-shift has nobody who can act; group legal no longer exists');
  });

  it('tells a person who left from one who was deactivated', () => {
    expect(nameCannotAct({ name: 'ada', kind: 'person', why: 'gone' })).toBe('ada is no longer here');
  });
});
