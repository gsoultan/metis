import type { ApiMigrationPlan, ApiNodeAction, ApiNodeMove, NodeActionKind } from '../services/types';

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

/** Everything a migration request says, as sent. */
export interface MigrationRequest {
  source: string;
  target: string;
  mapping: Record<string, string>;
  acknowledge: string[];
  actions: Record<string, ApiNodeAction>;
}

/**
 * A request as one comparable string, so a plan can be kept with the request
 * it answers. Built from what is sent rather than from the editor's rows: a
 * blank row changes nothing the server sees, and must not make the plan stale.
 */
export function migrationRequestKey(request: MigrationRequest): string {
  const sorted = <T>(record: Record<string, T>) => Object.entries(record).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return JSON.stringify([
    request.source,
    request.target,
    sorted(request.mapping),
    [...request.acknowledge].sort(),
    sorted(request.actions),
  ]);
}

/** What the apply button goes by. */
export interface ApplyState {
  /** The last plan worked out for these two versions. */
  plan: ApiMigrationPlan | null;
  /** Whether that plan answers exactly the request on screen. */
  fresh: boolean;
  /** Why the plan could not be worked out, when it could not. */
  error: string | null;
  /**
   * Whether an apply is already on its way. The button disables itself a
   * render after the press, which leaves room for a second one; the dialog
   * checks this in the press itself, before anything is sent.
   */
  applying: boolean;
}

/**
 * Whether pressing apply would apply the plan somebody is looking at.
 *
 * Only a fresh one. The dialog used to keep the last plan it had and leave
 * "Move" enabled on it while the plan for the next edit was on its way — so a
 * press in that moment applied a mapping, an acknowledgement or a target that
 * no plan on screen described. The server re-plans and refuses what is not
 * applicable, but an applicable plan nobody has read is exactly what a
 * preview-first dialog exists to prevent.
 */
export function canApply(state: ApplyState): boolean {
  return !state.applying && state.fresh && state.error === null
    && isApplicable(state.plan) && (state.plan?.instances ?? 0) > 0;
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

/** One row of the "decide this instead of moving it" editor. */
export interface ActionRow {
  from: string;
  kind: NodeActionKind | '';
  reason: string;
}

/**
 * Turns the action rows a person edits into what the API takes.
 *
 * A row with no reason is dropped rather than sent, for the same reason the
 * server refuses one: a skipped approval with no reason recorded is
 * indistinguishable on the trail from an approval somebody gave. Dropping it
 * here means the preview shows the refusal about the row they have not finished
 * rather than one about a node they never chose.
 */
export function toNodeActions(rows: readonly ActionRow[]): Record<string, ApiNodeAction> {
  const actions: Record<string, ApiNodeAction> = {};
  for (const row of rows) {
    const from = row.from.trim();
    const reason = row.reason.trim();
    if (from === '' || row.kind === '' || reason === '') continue;
    actions[from] = { kind: row.kind, reason };
  }
  return actions;
}

/**
 * One sentence about what a decision does, for the row that sets it.
 *
 * Written as the consequence rather than the verb: "skip" and "cancel" are
 * short enough to pick without reading, and what separates them is what happens
 * to the quotation, not what happens to the token.
 */
export function actionConsequence(kind: NodeActionKind, nodeID: string): string {
  if (kind === 'cancel') {
    return `Instances waiting at "${nodeID}" end here. They are not moved to the new version, and their record keeps the version they ran.`;
  }
  if (kind === 'hold') {
    return `Instances waiting at "${nodeID}" are left exactly as they are and raised as incidents, for somebody to decide one at a time.`;
  }
  return `Instances waiting at "${nodeID}" advance to the next step as though it had been done. Whoever holds the task loses it.`;
}
