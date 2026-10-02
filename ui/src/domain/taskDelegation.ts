/**
 * A task that has been delegated, what the inbox offers on it, and who has to
 * say why when they change whose work a task is.
 *
 * Delegating keeps the person who held the task as its owner. The delegate
 * works on it and hands it back; only the owner completes it.
 *
 * Whether a task is with a delegate is the server's to say and it says it in
 * one field: it sends `pending` only for a task that is waiting to be handed
 * back, and blanks a mark that was left over (entities.Task.DelegationForClients).
 * So the rule is not written a second time here.
 *
 * This decides only what is offered; the server decides what is allowed.
 */

import { hasRole, type RoleHolder } from './access';
import { PRIVILEGED_ROLE } from './roles';

/** The parts of a task that say whether it is with a delegate. */
export interface DelegableTask {
  delegationState?: string;
  assignee?: { username: string };
  owner?: { username: string };
}

/** Somebody looking at a task: who they are and what roles they hold. */
export type Viewer = RoleHolder & { username?: string };

const WITH_A_DELEGATE = 'pending';

/** The longest reason the server keeps (contracts.MaxHandOverReasonLength). */
export const REASON_LIMIT = 1000;

/**
 * Whether the task is with a delegate who has to hand it back before anybody
 * completes it.
 */
export function awaitsHandBack(task: DelegableTask): boolean {
  return task.delegationState === WITH_A_DELEGATE;
}

/**
 * Whether the viewer holds the task: they are its assignee. For a delegated
 * task that is the delegate, not the owner; a task with no assignee has no
 * holder.
 */
export function holdsTask(task: DelegableTask, viewer: string): boolean {
  return viewer !== '' && task.assignee?.username === viewer;
}

/** Whether the viewer is the delegate: the one to offer Hand back to, instead of Complete. */
export function handsBack(task: DelegableTask, viewer: string): boolean {
  return awaitsHandBack(task) && holdsTask(task, viewer);
}

/** Who delegated the task, or '' when nobody did. */
export function delegatedBy(task: DelegableTask): string {
  return task.owner?.username ?? '';
}

/**
 * How Hand back is offered to the viewer.
 *
 * `own`: they are the delegate, and it goes back at one press.
 * `withReason`: they are an administrator handing back somebody else's, which
 * the server takes only with a reason.
 * `none`: it is not theirs to hand back, or there is nothing to hand back.
 */
export type HandBackOffer = 'own' | 'withReason' | 'none';

export function handBackOffer(task: DelegableTask, viewer: Viewer | null | undefined): HandBackOffer {
  if (!awaitsHandBack(task)) return 'none';
  if (holdsTask(task, viewer?.username ?? '')) return 'own';
  return hasRole(viewer, PRIVILEGED_ROLE) ? 'withReason' : 'none';
}

/**
 * Something a task's holder does at one press — releasing it, handing it back
 * — being done by somebody else, who is asked why before it is sent.
 */
export interface ReasonRequest<T extends DelegableTask = DelegableTask> {
  kind: 'release' | 'handBack';
  task: T;
}

/**
 * Whether what has been written is enough to go ahead: nothing is needed from
 * the person holding the task, and anybody else has to have written a reason
 * the server will keep. Spaces around it are dropped before it is sent, so
 * they are not counted. Characters are counted as the server counts them.
 */
export function reasonReady(needed: boolean, written: string): boolean {
  const reason = written.trim();
  if ([...reason].length > REASON_LIMIT) return false;
  return !needed || reason !== '';
}

/** The reason to send with the request: what was written, trimmed, when one is needed. */
export function reasonToSend(needed: boolean, written: string): string | undefined {
  return needed ? written.trim() : undefined;
}
