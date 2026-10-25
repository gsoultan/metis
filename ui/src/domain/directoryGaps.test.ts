import { describe, expect, it } from 'bun:test';
import { fillableNames, fillsFrom, usedBy } from './directoryGaps';

describe('the switch-over report', () => {
  const gaps = [
    { name: 'dana', kind: 'person' as const, open_tasks: 2, has_account: true, display_name: 'Dana Scully' },
    { name: 'ghost', kind: 'person' as const, open_tasks: 0, models: ['claims'] },
    { name: 'reviewers', kind: 'group' as const, open_tasks: 1, has_platform_group: true, members: ['dana', 'eve'] },
  ];

  it('fills only what the organization has something to fill from', () => {
    expect(fillableNames(gaps)).toEqual({ people: ['dana'], groups: ['reviewers'] });
  });

  it('says where a name is used', () => {
    expect(usedBy(gaps[0])).toBe('2 open tasks');
    expect(usedBy(gaps[1])).toBe('model claims');
  });

  it('says what it would be made from', () => {
    expect(fillsFrom(gaps[0])).toBe('From the User Dana Scully (dana)');
    expect(fillsFrom(gaps[1])).toContain('import them');
    expect(fillsFrom(gaps[2])).toContain('dana, eve');
  });
});
