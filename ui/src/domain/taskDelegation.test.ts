import { describe, expect, it } from 'bun:test';

import {
  REASON_LIMIT,
  awaitsHandBack,
  delegatedBy,
  handBackOffer,
  handsBack,
  holdsTask,
  reasonReady,
  reasonToSend,
  type DelegableTask,
} from './taskDelegation';

const delegated: DelegableTask = {
  delegationState: 'pending',
  assignee: { username: 'mallory' },
  owner: { username: 'budi' },
};

const administrator = { username: 'ani', roles: ['ADMIN'] };
const member = { username: 'citra', roles: ['USER'] };

/*
 * A delegated task is with somebody who works on it and is not theirs to
 * complete: the server refuses the completion and says to hand it back. The
 * inbox offered Complete on it all the same.
 */
describe('a task that is with a delegate', () => {
  it('is waiting to be handed back', () => {
    expect(awaitsHandBack(delegated)).toBe(true);
    expect(delegatedBy(delegated)).toBe('budi');
  });

  it('is the delegate’s to hand back, and nobody else’s', () => {
    expect(handsBack(delegated, 'mallory')).toBe(true);
    expect(handsBack(delegated, 'budi')).toBe(false);
    expect(handsBack(delegated, '')).toBe(false);
  });

  it.each([
    ['it was never delegated', { delegationState: '', assignee: { username: 'mallory' } }],
    ['it has been handed back', { delegationState: 'resolved', assignee: { username: 'budi' }, owner: { username: 'budi' } }],
    // The server sends no state at all for a mark that no longer means anything.
    ['the server sent no delegation', { assignee: { username: 'mallory' } }],
  ])('is not one when %s', (_, task) => {
    expect(awaitsHandBack(task as DelegableTask)).toBe(false);
    expect(handsBack(task as DelegableTask, 'mallory')).toBe(false);
    expect(handBackOffer(task as DelegableTask, { username: 'mallory', roles: ['ADMIN'] })).toBe('none');
  });
});

/*
 * The server takes a hand-back from the delegate, and from an administrator
 * who says why. Anybody else is refused, so nobody else is offered it.
 */
describe('who is offered Hand back', () => {
  it('is the delegate, who need not explain it', () => {
    expect(handBackOffer(delegated, { username: 'mallory', roles: ['USER'] })).toBe('own');
    // An administrator who is the delegate holds the task like anyone else.
    expect(handBackOffer(delegated, { username: 'mallory', roles: ['ADMIN'] })).toBe('own');
  });

  it('is an administrator, who has to say why', () => {
    expect(handBackOffer(delegated, administrator)).toBe('withReason');
  });

  it('is nobody else: not its owner, not a bystander, not somebody signed out', () => {
    expect(handBackOffer(delegated, { username: 'budi', roles: ['USER'] })).toBe('none');
    expect(handBackOffer(delegated, member)).toBe('none');
    expect(handBackOffer(delegated, { username: 'ops', roles: ['OPERATOR'] })).toBe('none');
    expect(handBackOffer(delegated, null)).toBe('none');
  });
});

/*
 * Reassigning, editing, releasing and handing back are refused without a
 * reason from anyone but the person holding the task. The inbox sent none, so
 * every one of those failed for an administrator acting on somebody else's.
 */
describe('who has to say why', () => {
  it('is everybody but the person holding the task', () => {
    expect(holdsTask(delegated, 'mallory')).toBe(true);
    expect(holdsTask(delegated, 'budi')).toBe(false);
    // Nobody holds a task with no assignee, and nobody is signed in as ''.
    expect(holdsTask({}, 'mallory')).toBe(false);
    expect(holdsTask({ assignee: { username: '' } }, '')).toBe(false);
  });

  it('lets the holder go ahead with nothing written', () => {
    expect(reasonReady(false, '')).toBe(true);
  });

  it('holds anybody else until they have written something that is not just spaces', () => {
    expect(reasonReady(true, '')).toBe(false);
    expect(reasonReady(true, '   \n ')).toBe(false);
    expect(reasonReady(true, 'Mallory is on leave')).toBe(true);
  });

  it('holds a reason longer than the server keeps', () => {
    expect(REASON_LIMIT).toBe(1000);
    expect(reasonReady(true, 'x'.repeat(REASON_LIMIT))).toBe(true);
    expect(reasonReady(true, 'x'.repeat(REASON_LIMIT + 1))).toBe(false);
    // Spaces around it are not kept, so they do not count.
    expect(reasonReady(true, ` ${'x'.repeat(REASON_LIMIT)} `)).toBe(true);
  });

  it('sends what was written without the spaces around it, and nothing for the holder', () => {
    expect(reasonToSend(true, '  Mallory is on leave \n')).toBe('Mallory is on leave');
    // The holder is not asked, so nothing they could not see is sent for them.
    expect(reasonToSend(false, 'left over from another task')).toBeUndefined();
  });
});
