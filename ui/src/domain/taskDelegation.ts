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
import { fallsToOperators, takesUnnamedWork, type TaskAssignment } from './unnamedTask';

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
 * How something its holder does at one press is offered to the viewer.
 *
 * `own`: they hold the task, and it is done at one press.
 * `withReason`: they are an administrator doing it to a task somebody else
 * holds, which the server takes only with a reason.
 * `none`: the server would refuse them whatever they wrote, so it is not
 * offered.
 */
export type Offer = 'own' | 'withReason' | 'none';

/** How Hand back is offered. */
export type HandBackOffer = Offer;

function isAdministrator(viewer: Viewer | null | undefined): boolean {
  return hasRole(viewer, PRIVILEGED_ROLE);
}

function holds(task: DelegableTask, viewer: Viewer | null | undefined): boolean {
  return holdsTask(task, viewer?.username ?? '');
}

/**
 * How Hand back is offered to the viewer: to the delegate at one press, to an
 * administrator with a reason, and to nobody else (handOverCaller.mayResolve).
 */
export function handBackOffer(task: DelegableTask, viewer: Viewer | null | undefined): HandBackOffer {
  if (!awaitsHandBack(task)) return 'none';
  if (holds(task, viewer)) return 'own';
  return isAdministrator(viewer) ? 'withReason' : 'none';
}

/**
 * How Release is offered to the viewer: to the holder at one press, to an
 * administrator with a reason, and to nobody else (handOverCaller.mayRelease).
 * A task nobody holds has nothing to release, and one with a delegate is
 * handed back rather than released.
 */
export function releaseOffer(task: DelegableTask, viewer: Viewer | null | undefined): Offer {
  if (awaitsHandBack(task) || !task.assignee?.username) return 'none';
  if (holds(task, viewer)) return 'own';
  return isAdministrator(viewer) ? 'withReason' : 'none';
}

/**
 * Whether to offer the viewer Edit — a task's name, priority and due date are
 * its holder's or an administrator's to change (handOverCaller.mayEdit).
 */
export function offersEdit(task: DelegableTask, viewer: Viewer | null | undefined): boolean {
  return holds(task, viewer) || isAdministrator(viewer);
}

/**
 * Whether to offer the viewer Reassign: the holder and an administrator may
 * give a task to somebody, and an operator may when nobody was named for it
 * (handOverCaller.mayHandOver). Nobody may while it is with a delegate: it
 * goes back to its owner first.
 */
export function offersReassign(task: DelegableTask & TaskAssignment, viewer: Viewer | null | undefined): boolean {
  if (awaitsHandBack(task)) return false;
  if (holds(task, viewer) || isAdministrator(viewer)) return true;
  return fallsToOperators(task) && takesUnnamedWork(viewer);
}

/**
 * Whether somebody has to say why.
 *
 * `required`: the server refuses them without a reason.
 * `optional`: it asks for one only in a case the dialog cannot see coming.
 * `none`: it never asks them, so they are shown no field.
 */
export type ReasonNeed = 'required' | 'optional' | 'none';

/**
 * Whether the viewer has to say why they are changing the task: everybody but
 * the person holding it does (handOverCaller.reasonFor).
 */
export function reasonNeed(task: DelegableTask, viewer: Viewer | null | undefined): ReasonNeed {
  return holds(task, viewer) ? 'none' : 'required';
}

/**
 * Whether the viewer has to say why they are reassigning the task.
 *
 * As reasonNeed, with one more case. An administrator may give a task to
 * somebody it was not offered to, and the server wants a reason for that even
 * from an administrator who holds the task (taskService.admitCandidacy). Who
 * it was offered to includes teams whose members the browser does not know, so
 * the field is there for them to fill in when that is what they are doing.
 * Anybody else who holds the task is not asked: the server does not take a
 * reason from them, it tells them an administrator can do it.
 */
export function reassignReasonNeed(task: DelegableTask, viewer: Viewer | null | undefined): ReasonNeed {
  if (!holds(task, viewer)) return 'required';
  return isAdministrator(viewer) ? 'optional' : 'none';
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
 * Whether what has been written is enough to go ahead: somebody who must say
 * why has to have written a reason, and nobody may send one longer than the
 * server keeps. Spaces around it are dropped before it is sent, so they are
 * not counted. Characters are counted as the server counts them.
 */
export function reasonReady(need: ReasonNeed, written: string): boolean {
  const reason = written.trim();
  if ([...reason].length > REASON_LIMIT) return false;
  return need !== 'required' || reason !== '';
}

/**
 * The reason to send with the request: what was written, trimmed — or nothing,
 * when nothing was written or the writer was never asked.
 */
export function reasonToSend(need: ReasonNeed, written: string): string | undefined {
  if (need === 'none') return undefined;
  return written.trim() || undefined;
}

/**
 * Which of the selected tasks "Release selected" sends. One that is with the
 * reader as a delegate is handed back, not released: the server refuses it, so
 * it is held back and the reader is told. A task this does not know is sent,
 * and the server answers for it.
 */
export function splitReleasable(
  tasks: readonly (DelegableTask & { id: string })[],
  selected: readonly string[],
): { release: string[]; heldBack: string[] } {
  const withDelegate = new Set(tasks.filter(awaitsHandBack).map((task) => task.id));
  return {
    release: selected.filter((id) => !withDelegate.has(id)),
    heldBack: selected.filter((id) => withDelegate.has(id)),
  };
}
