import type { StrandedName, StrandedTask } from '../services/domains/participantService';

/** Why one name can no longer act, in a few words. */
export function nameCannotAct(n: StrandedName): string {
  if (n.kind === 'group') {
    return n.why === 'empty' ? `group ${n.name} has nobody who can act` : `group ${n.name} no longer exists`;
  }
  return n.why === 'inactive' ? `${n.name} was deactivated` : `${n.name} is no longer here`;
}

/** Why nobody can act on a task: who holds it, or who it is offered to. */
export function whyNobodyCanAct(task: StrandedTask): string {
  if (task.kind === 'assignee_inactive') return `Held by ${task.assignee}, who was deactivated`;
  if (task.kind === 'assignee_gone') return `Held by ${task.assignee}, who is no longer here`;
  const names = task.missing.map(nameCannotAct);
  if (names.length === 0) return 'Offered to nobody who can act';
  return `Nobody left to take it: ${names.join('; ')}`;
}
