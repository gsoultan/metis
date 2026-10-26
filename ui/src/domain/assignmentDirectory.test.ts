import { describe, expect, it } from 'bun:test';
import { directoryOf, groupOptions, isExpression, peopleOptions } from './assignmentDirectory';
import { unknownNameIssues, validateProcess } from './processValidation';
import type { Participant } from './participantImport';

const ada: Participant = { id: '1', username: 'ada', display_name: 'Ada Lovelace', active: true, has_credentials: true };
const gone: Participant = { id: '2', username: 'bob', active: false, has_credentials: false };
const approvers = { id: 'g-1', name: 'approvers' };

describe('the picker lists the People of the project', () => {
  it('lists active People by name and keeps a name the step carries that is not one, marked', () => {
    const options = peopleOptions([ada, gone], ['dana']);
    expect(options.map((o) => o.value)).toEqual(['ada', 'dana']);
    expect(options[0].label).toBe('Ada Lovelace (ada)');
    expect(options[1].label).toContain('not in this project');
  });

  it('lists People groups, and knows a group named by its id', () => {
    expect(groupOptions([approvers], ['g-1']).map((o) => o.value)).toEqual(['approvers']);
    expect(groupOptions([approvers], ['finance']).map((o) => o.value)).toEqual(['approvers', 'finance']);
  });
});

describe('a step naming somebody the directory does not have', () => {
  const directory = directoryOf([ada, gone], [approvers]);
  const step = (data: Record<string, unknown>) => ({ id: 's1', type: 'userTask', data: { label: 'Approve', ...data } });

  it('is warned about, by name', () => {
    const issues = unknownNameIssues([step({ assignee: 'dana' })], directory);
    expect(issues).toHaveLength(1);
    expect(issues[0].severity).toBe('warning');
    expect(issues[0].message).toContain('"dana"');
    expect(issues[0].id).toBe('s1');
  });

  it('counts a deactivated person as not there', () => {
    expect(unknownNameIssues([step({ candidateUsers: ['bob'] })], directory)).toHaveLength(1);
  });

  it('warns about a team that is not a People group', () => {
    const issues = unknownNameIssues([step({ candidateGroups: ['approvers', 'finance'] })], directory);
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toContain('"finance"');
    expect(issues[0].message).not.toContain('"approvers"');
  });

  it('says nothing of People, groups by id, expressions or blank names', () => {
    expect(unknownNameIssues([step({ assignee: 'ada' })], directory)).toEqual([]);
    expect(unknownNameIssues([step({ candidateGroups: ['g-1'] })], directory)).toEqual([]);
    expect(unknownNameIssues([step({ assignee: '${initiator}' })], directory)).toEqual([]);
    expect(unknownNameIssues([step({ candidateUsers: [' '] })], directory)).toEqual([]);
    expect(isExpression('#{owner}')).toBe(true);
  });

  it('is not checked until the directory is known', () => {
    const nodes = [step({ assignee: 'dana' })];
    const unchecked = validateProcess(nodes, []).filter((i) => i.message.includes('"dana"'));
    const checked = validateProcess(nodes, [], new Map(), directory).filter((i) => i.message.includes('"dana"'));
    expect(unchecked).toEqual([]);
    expect(checked).toHaveLength(1);
  });
});
