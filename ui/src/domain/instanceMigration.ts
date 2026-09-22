import type { ApiMigrationPlan, ApiNodeMove } from '../services/types';

/**
 * Reading a migration plan.
 *
 * Moving running instances rewrites work that is already somebody's — a
 * purchase order halfway through approval, a leave request sitting in an
 * inbox. The decidable parts of presenting that live here rather than in the
 * modal, because "would this strand anybody's task" is a question with an
 * answer, not a rendering detail.
 */

/** How much work sits on one node, across all three kinds. */
export function workOn(move: ApiNodeMove): number {
  return move.tokens + move.tasks + move.jobs;
}

/** The nodes that would actually move, in plan order. */
export function movedNodes(plan: ApiMigrationPlan): ApiNodeMove[] {
  return (plan.moves ?? []).filter((move) => move.from !== move.to);
}

/**
 * The nodes carried across unchanged because the new version still has them.
 *
 * Shown, not hidden: this is how somebody confirms they did not need a mapping
 * for a node rather than forgot one.
 */
export function carriedNodes(plan: ApiMigrationPlan): ApiNodeMove[] {
  return (plan.moves ?? []).filter((move) => move.from === move.to);
}

/** Whether applying this plan would be accepted. */
export function isApplicable(plan: ApiMigrationPlan | null): boolean {
  return plan !== null && (plan.refusals?.length ?? 0) === 0;
}

/** Total tasks that would move — the number that means "people affected". */
export function tasksAffected(plan: ApiMigrationPlan): number {
  return (plan.moves ?? []).reduce((total, move) => total + move.tasks, 0);
}

/**
 * Tasks somebody is holding right now that would be returned to the queue.
 *
 * Separate from tasksAffected because it is a different conversation: an
 * unclaimed task moving is bookkeeping, and a claimed one moving is a person
 * who was partway through something losing their place.
 */
export function heldTasksAffected(plan: ApiMigrationPlan): number {
  return movedNodes(plan).reduce(
    (total, move) => total + (move.tasks_claimed ?? 0) + (move.tasks_delegated ?? 0),
    0,
  );
}

/**
 * Whether there is anything to look at that is not a refusal.
 *
 * A plan can be perfectly applicable and still deserve a second look — a
 * removed step whose form fed a gateway downstream is the case that motivated
 * this, because it applies cleanly and then produces incidents.
 */
export function hasAdvisories(plan: ApiMigrationPlan | null): boolean {
  if (plan === null) return false;
  return (plan.warnings?.length ?? 0) > 0 || (plan.removed_nodes?.length ?? 0) > 0;
}

/**
 * One sentence about what the new version dropped.
 *
 * Phrased as a consequence rather than a list, because the list on its own
 * reads as trivia: the point of naming a removed user task is that whatever its
 * form used to write is no longer being written.
 */
export function removedNodesSummary(plan: ApiMigrationPlan): string | null {
  const removed = plan.removed_nodes ?? [];
  if (removed.length === 0) return null;
  const nodes = removed.map((node) => `"${node}"`).join(', ');
  if (removed.length === 1) {
    return `Version ${plan.target_version} no longer has ${nodes}. Anything that step used to set is no longer set.`;
  }
  return `Version ${plan.target_version} no longer has ${nodes}. Anything those steps used to set is no longer set.`;
}

/**
 * One sentence for the top of the dialog.
 *
 * Written to be readable when the answer is "nothing": a plan over zero
 * instances is the common case once a version has drained, and telling somebody
 * "0 instances would move" is clearer than an empty table.
 */
export function planSummary(plan: ApiMigrationPlan): string {
  if (plan.instances === 0) {
    return `Nothing is running on version ${plan.source_version}. There is nothing to move.`;
  }
  const instances = plan.instances === 1 ? '1 instance' : `${plan.instances} instances`;
  const tasks = tasksAffected(plan);
  if (tasks === 0) {
    return `${instances} would move from version ${plan.source_version} to version ${plan.target_version}.`;
  }
  const inboxes = tasks === 1 ? '1 task' : `${tasks} tasks`;
  return `${instances} would move from version ${plan.source_version} to version ${plan.target_version}, including ${inboxes} already in somebody's inbox.`;
}

/**
 * Turns the mapping rows a person edits into what the API takes.
 *
 * Blank targets are dropped rather than sent as empty strings: leaving a row
 * empty means "I have not decided", and sending it would ask the server to move
 * work onto a node called "".
 */
export function toNodeMapping(rows: readonly { from: string; to: string }[]): Record<string, string> {
  const mapping: Record<string, string> = {};
  for (const row of rows) {
    const from = row.from.trim();
    const to = row.to.trim();
    if (from === '' || to === '' || from === to) continue;
    mapping[from] = to;
  }
  return mapping;
}
