import { describe, expect, it } from 'bun:test';

import { fallsToOperators, offersClaim, takesUnnamedWork, type TaskAssignment } from './unnamedTask';

const task = (overrides: Partial<TaskAssignment> = {}): TaskAssignment => ({
  type: 'userTask',
  status: 'unclaimed',
  assignee: undefined,
  candidateUsers: [],
  candidateGroups: [],
  ...overrides,
});

const member = { roles: ['USER'] };
const operator = { roles: ['OPERATOR'] };
const administrator = { roles: ['admin'] };

/*
 * A task with no assignee and no candidates is not everybody's. Absent
 * constraint means deny, so the server lets only an administrator or an
 * operator take it — the mirror here decides what the board offers.
 */
describe('a task nobody was named for', () => {
  it('falls to the operators', () => {
    expect(fallsToOperators(task())).toBe(true);
  });

  it.each([
    ['somebody holds it', { assignee: { username: 'dana' } }],
    ['people are offered it', { candidateUsers: [{ username: 'dana' }] }],
    ['a team is offered it', { candidateGroups: [{ name: 'finance' }] }],
  ])('is not one when %s', (_, overrides) => {
    expect(fallsToOperators(task(overrides as Partial<TaskAssignment>))).toBe(false);
  });

  /* The designer has no field to name anybody for a manual step, and says an empty one is anybody's. */
  it('is not one when it is a manual step', () => {
    expect(fallsToOperators(task({ type: 'manualTask' }))).toBe(false);
  });
});

describe('who may take one', () => {
  it('is an administrator or an operator, whatever case the role is in', () => {
    expect(takesUnnamedWork(operator)).toBe(true);
    expect(takesUnnamedWork(administrator)).toBe(true);
  });

  it('is nobody else, and nobody signed out', () => {
    expect(takesUnnamedWork(member)).toBe(false);
    expect(takesUnnamedWork(null)).toBe(false);
  });
});

describe('what the board offers to claim', () => {
  it('does not offer a member a task nobody was named for', () => {
    expect(offersClaim(task(), member)).toBe(false);
  });

  it('offers it to an administrator or an operator', () => {
    expect(offersClaim(task(), operator)).toBe(true);
    expect(offersClaim(task(), administrator)).toBe(true);
  });

  it('offers anybody an unclaimed task offered to people, which the server then checks', () => {
    expect(offersClaim(task({ candidateGroups: [{ name: 'finance' }] }), member)).toBe(true);
  });

  it('offers nothing that is no longer waiting to be claimed', () => {
    expect(offersClaim(task({ status: 'claimed', assignee: { username: 'dana' } }), operator)).toBe(false);
  });
});
