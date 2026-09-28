import { describe, expect, it } from 'bun:test';

import { fallsToOperators, offersClaim, takesUnnamedWork, type TaskAssignment } from './unnamedTask';

/** A task as the board has it, kind and all: the kind says nothing about who it is for. */
type BoardTask = TaskAssignment & { type: string };

const task = (overrides: Partial<BoardTask> = {}): BoardTask => ({
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
    expect(fallsToOperators(task(overrides as Partial<BoardTask>))).toBe(false);
  });

  /* A manual step names people the way a user step does, and is held to the same rule. */
  it('is one when it is a manual step too', () => {
    expect(fallsToOperators(task({ type: 'manualTask' }))).toBe(true);
  });

  it('is not one when a manual step names somebody', () => {
    expect(fallsToOperators(task({ type: 'manualTask', candidateGroups: [{ name: 'warehouse' }] }))).toBe(false);
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

  it('does the same with a manual step nobody was named for', () => {
    const manual = task({ type: 'manualTask' });
    expect(offersClaim(manual, member)).toBe(false);
    expect(offersClaim(manual, operator)).toBe(true);
    expect(offersClaim(manual, administrator)).toBe(true);
  });

  it('offers anybody an unclaimed task offered to people, which the server then checks', () => {
    expect(offersClaim(task({ candidateGroups: [{ name: 'finance' }] }), member)).toBe(true);
  });

  it('offers nothing that is no longer waiting to be claimed', () => {
    expect(offersClaim(task({ status: 'claimed', assignee: { username: 'dana' } }), operator)).toBe(false);
  });
});
