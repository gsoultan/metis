import { describe, expect, it } from 'bun:test';

import {
  REASON_LIMIT,
  awaitsHandBack,
  delegatedBy,
  handBackOffer,
  handsBack,
  holdsTask,
  offersEdit,
  offersReassign,
  reasonNeed,
  reasonReady,
  reasonToSend,
  reassignReasonNeed,
  releaseOffer,
  splitReleasable,
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
  const held = { assignee: { username: 'mallory' } };

  it('is everybody but the person holding the task', () => {
    expect(holdsTask(delegated, 'mallory')).toBe(true);
    expect(holdsTask(delegated, 'budi')).toBe(false);
    // Nobody holds a task with no assignee, and nobody is signed in as ''.
    expect(holdsTask({}, 'mallory')).toBe(false);
    expect(holdsTask({ assignee: { username: '' } }, '')).toBe(false);

    expect(reasonNeed(held, { username: 'mallory', roles: ['USER'] })).toBe('none');
    expect(reasonNeed(held, administrator)).toBe('required');
    expect(reasonNeed({}, administrator)).toBe('required');
    expect(reasonNeed(held, null)).toBe('required');
  });

  /*
   * An administrator may give a task to somebody it was not offered to, and
   * the server takes that only with a reason — whether or not the
   * administrator holds the task. The dialog asked the holder for nothing, so
   * an administrator moving their own task there was told to say why and had
   * nowhere to say it.
   */
  it('lets an administrator who holds the task say why when reassigning it, without making them', () => {
    expect(reassignReasonNeed(held, { username: 'mallory', roles: ['ADMIN'] })).toBe('optional');
    // Anybody else who holds it is not asked: the server does not take a
    // reason from them, it tells them an administrator can do it.
    expect(reassignReasonNeed(held, { username: 'mallory', roles: ['USER'] })).toBe('none');
    expect(reassignReasonNeed(held, { username: 'mallory', roles: ['OPERATOR'] })).toBe('none');
    expect(reassignReasonNeed(held, administrator)).toBe('required');
    expect(reassignReasonNeed({}, { username: 'ops', roles: ['OPERATOR'] })).toBe('required');
  });

  it('lets somebody who is not asked, or need not answer, go ahead with nothing written', () => {
    expect(reasonReady('none', '')).toBe(true);
    expect(reasonReady('optional', '')).toBe(true);
    expect(reasonReady('optional', '  ')).toBe(true);
  });

  it('holds anybody who must until they have written something that is not just spaces', () => {
    expect(reasonReady('required', '')).toBe(false);
    expect(reasonReady('required', '   \n ')).toBe(false);
    expect(reasonReady('required', 'Mallory is on leave')).toBe(true);
  });

  it('holds a reason longer than the server keeps', () => {
    expect(REASON_LIMIT).toBe(1000);
    expect(reasonReady('required', 'x'.repeat(REASON_LIMIT))).toBe(true);
    expect(reasonReady('required', 'x'.repeat(REASON_LIMIT + 1))).toBe(false);
    expect(reasonReady('optional', 'x'.repeat(REASON_LIMIT + 1))).toBe(false);
    // Spaces around it are not kept, so they do not count.
    expect(reasonReady('required', ` ${'x'.repeat(REASON_LIMIT)} `)).toBe(true);
  });

  it('sends what was written without the spaces around it, and nothing when nothing was', () => {
    expect(reasonToSend('required', '  Mallory is on leave \n')).toBe('Mallory is on leave');
    expect(reasonToSend('optional', ' Citra knows this supplier ')).toBe('Citra knows this supplier');
    expect(reasonToSend('optional', '   ')).toBeUndefined();
    // Somebody who was not asked sends nothing they could not see.
    expect(reasonToSend('none', 'left over from another task')).toBeUndefined();
  });
});

/*
 * The inbox offered Edit and Reassign on every task to everybody, and asked a
 * reason of people the server refuses whatever they write. What is offered
 * follows the server's rules: handOverCaller.mayEdit, mayHandOver and
 * mayRelease.
 */
describe('what is offered on a task', () => {
  const user = (username: string) => ({ username, roles: ['USER'] });
  const operator = { username: 'ops', roles: ['OPERATOR'] };
  const heldByMallory = { status: 'claimed', assignee: { username: 'mallory' }, candidateUsers: [], candidateGroups: [] };
  const nobodyNamed = { status: 'unclaimed', candidateUsers: [], candidateGroups: [] };
  const offeredToFinance = { status: 'unclaimed', candidateUsers: [], candidateGroups: [{ name: 'finance' }] };
  const offeredToCitra = { status: 'unclaimed', candidateUsers: [{ username: 'citra' }], candidateGroups: [] };

  it('offers Edit to the person holding it and to an administrator, and to nobody else', () => {
    expect(offersEdit(heldByMallory, user('mallory'))).toBe(true);
    expect(offersEdit(heldByMallory, administrator)).toBe(true);
    expect(offersEdit(nobodyNamed, administrator)).toBe(true);
    expect(offersEdit(heldByMallory, user('citra'))).toBe(false);
    expect(offersEdit(heldByMallory, operator)).toBe(false);
    // Not even on a task nobody was named for: an operator may take one, not change it.
    expect(offersEdit(nobodyNamed, operator)).toBe(false);
    expect(offersEdit(heldByMallory, null)).toBe(false);
  });

  it('offers Reassign to the person holding it and to an administrator', () => {
    expect(offersReassign(heldByMallory, user('mallory'))).toBe(true);
    expect(offersReassign(heldByMallory, administrator)).toBe(true);
    expect(offersReassign(offeredToFinance, administrator)).toBe(true);
    expect(offersReassign(heldByMallory, user('citra'))).toBe(false);
    expect(offersReassign(heldByMallory, null)).toBe(false);
  });

  it('offers Reassign to an operator only on a task nobody was named for', () => {
    expect(offersReassign(nobodyNamed, operator)).toBe(true);
    expect(offersReassign(offeredToFinance, operator)).toBe(false);
    expect(offersReassign(offeredToCitra, operator)).toBe(false);
    expect(offersReassign(heldByMallory, operator)).toBe(false);
    // Nobody being named is not everybody being named.
    expect(offersReassign(nobodyNamed, user('citra'))).toBe(false);
    // Being a candidate is not holding it.
    expect(offersReassign(offeredToCitra, user('citra'))).toBe(false);
  });

  it('offers Reassign to nobody while the task is with a delegate', () => {
    const withDelegate = { ...heldByMallory, status: 'delegated', delegationState: 'pending', owner: { username: 'budi' } };
    expect(offersReassign(withDelegate, user('mallory'))).toBe(false);
    expect(offersReassign(withDelegate, administrator)).toBe(false);
  });

  it('offers Release to its holder at one press, to an administrator with a reason, and to nobody else', () => {
    expect(releaseOffer(heldByMallory, user('mallory'))).toBe('own');
    expect(releaseOffer(heldByMallory, { username: 'mallory', roles: ['ADMIN'] })).toBe('own');
    expect(releaseOffer(heldByMallory, administrator)).toBe('withReason');
    expect(releaseOffer(heldByMallory, user('citra'))).toBe('none');
    expect(releaseOffer(heldByMallory, operator)).toBe('none');
    expect(releaseOffer(heldByMallory, null)).toBe('none');
  });

  it('offers Release on nothing that cannot be released: a task nobody holds, or one with a delegate', () => {
    expect(releaseOffer(nobodyNamed, administrator)).toBe('none');
    expect(releaseOffer(delegated, { username: 'mallory', roles: ['USER'] })).toBe('none');
    expect(releaseOffer(delegated, administrator)).toBe('none');
  });
});

/*
 * "Release selected" sent every ticked task. One delegated to the reader is
 * handed back, not released, so it earned a refusal every time.
 */
describe('releasing a selection', () => {
  const tasks = [
    { id: 't1', assignee: { username: 'mallory' } },
    { id: 't2', assignee: { username: 'mallory' }, delegationState: 'pending', owner: { username: 'budi' } },
    { id: 't3', assignee: { username: 'mallory' }, delegationState: 'resolved', owner: { username: 'mallory' } },
  ];

  it('leaves out what is with the reader as a delegate, and says which', () => {
    expect(splitReleasable(tasks, ['t1', 't2', 't3'])).toEqual({ release: ['t1', 't3'], heldBack: ['t2'] });
  });

  it('sends a task it knows nothing about: the server answers for it', () => {
    expect(splitReleasable(tasks, ['t9'])).toEqual({ release: ['t9'], heldBack: [] });
    expect(splitReleasable(tasks, [])).toEqual({ release: [], heldBack: [] });
  });
});
