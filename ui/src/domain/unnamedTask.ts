/**
 * A task nobody was named for, and who may take one.
 *
 * A task with no assignee, no candidate users and no candidate groups is not
 * everybody's. Absent constraint means deny, so the server lets only an
 * administrator or an operator claim it, complete it or give it to somebody.
 * A manual step is the exception: the designer has no field to name anybody
 * for one, and tells its author that an empty one is anybody's. This mirrors
 * entities.Task.FallsToOperators and entities.TakesUnnamedWork on the server.
 *
 * It decides only what is offered; the server decides what is allowed. An
 * installation that has brought the old rule back
 * (METIS_ALLOW_UNASSIGNED_TASK_CLAIMS) lets anybody take such a task, and the
 * inbox's Available to Claim — which the server fills — offers it to them
 * there.
 */

import { hasRole, type RoleHolder } from './access';
import { OPERATOR_ROLE, PRIVILEGED_ROLE } from './roles';

/** The parts of a task that say who it is for. */
export interface TaskAssignment {
  type: string;
  status: string;
  assignee?: { username: string };
  candidateUsers: readonly { username: string }[];
  candidateGroups: readonly { name: string }[];
}

const MANUAL_STEP = 'manualTask';
const WAITING_TO_BE_CLAIMED = 'unclaimed';

/** Whether nobody was named for the task, so that only an administrator or an operator may take it. */
export function fallsToOperators(task: TaskAssignment): boolean {
  const namesNobody = !task.assignee?.username && task.candidateUsers.length === 0 && task.candidateGroups.length === 0;
  return task.type !== MANUAL_STEP && namesNobody;
}

/** Whether somebody may take a task nobody was named for: an administrator or an operator. */
export function takesUnnamedWork(viewer: RoleHolder | null | undefined): boolean {
  return hasRole(viewer, PRIVILEGED_ROLE) || hasRole(viewer, OPERATOR_ROLE);
}

/**
 * Whether to offer the viewer "Claim" on a task: one waiting to be claimed,
 * unless nobody was named for it and the viewer may not take such a task.
 *
 * Work offered to people or teams is offered as before; whether the viewer is
 * among them is the server's to check.
 */
export function offersClaim(task: TaskAssignment, viewer: RoleHolder | null | undefined): boolean {
  if (task.status !== WAITING_TO_BE_CLAIMED) {
    return false;
  }
  return !fallsToOperators(task) || takesUnnamedWork(viewer);
}
