import type { ApiDefinition, ApiNode } from '../services/types';

/**
 * What changed between two versions of a process.
 *
 * Migrating running work means telling the engine where each node's work should
 * go, and until now that was typed: two free-text boxes per row, node ids
 * copied by eye from a diagram in another tab. The planner refuses a mapping
 * that names a node the target does not have, which is the right answer to a
 * typo and a poor way to find out about one.
 *
 * Both versions are already fetchable by id, so the comparison is decidable in
 * the browser and belongs here rather than in the dialog — same reason
 * instanceMigration.ts exists.
 */

/** How one node differs between the two versions. */
export type NodeChangeKind =
  /** In the old version and not the new one. Its work needs somewhere to go. */
  | 'removed'
  /** In the new version and not the old one. Nothing is parked on it yet. */
  | 'added'
  /** In both, with something about it different. */
  | 'changed'
  /** In both and identical. */
  | 'unchanged';

export interface NodeChange {
  id: string;
  kind: NodeChangeKind;
  /** The node as the old version has it, absent when it is new. */
  before?: ApiNode;
  /** The node as the new version has it, absent when it was removed. */
  after?: ApiNode;
  /**
   * What differs, in words, for a node that is in both.
   *
   * Only the properties that change what the node *does* or who does it — a
   * node that moved on the canvas has not changed in any sense a migration
   * cares about, and saying so would bury the ones that did.
   */
  differences: string[];
}

export interface VersionDiff {
  changes: NodeChange[];
}

/** The fields worth comparing, and how to read each one out of a node. */
const COMPARED: ReadonlyArray<{ label: string; read: (node: ApiNode) => string }> = [
  { label: 'name', read: (n) => n.name ?? '' },
  { label: 'type', read: (n) => n.type ?? '' },
  { label: 'assignee', read: (n) => n.assignee ?? '' },
  { label: 'candidate users', read: (n) => (n.candidate_users ?? []).map((u) => u.username).sort().join(', ') },
  { label: 'candidate groups', read: (n) => (n.candidate_groups ?? []).map((g) => g.name).sort().join(', ') },
  { label: 'due date', read: (n) => n.due_date ?? '' },
  { label: 'form', read: (n) => n.form_key ?? '' },
];

/**
 * Compares two versions node by node.
 *
 * Node id is identity, which is what the engine uses too: a mapping is from one
 * id to another, and a node that keeps its id across a version is the same node
 * as far as running work is concerned however much else about it changed.
 */
export function diffVersions(before: ApiDefinition | null, after: ApiDefinition | null): VersionDiff {
  const beforeNodes = new Map((before?.nodes ?? []).map((node) => [node.id, node]));
  const afterNodes = new Map((after?.nodes ?? []).map((node) => [node.id, node]));

  const changes: NodeChange[] = [];

  for (const [id, node] of beforeNodes) {
    const counterpart = afterNodes.get(id);
    if (!counterpart) {
      changes.push({ id, kind: 'removed', before: node, differences: [] });
      continue;
    }
    const differences = describeDifferences(node, counterpart);
    changes.push({
      id,
      kind: differences.length > 0 ? 'changed' : 'unchanged',
      before: node,
      after: counterpart,
      differences,
    });
  }

  for (const [id, node] of afterNodes) {
    if (beforeNodes.has(id)) continue;
    changes.push({ id, kind: 'added', after: node, differences: [] });
  }

  changes.sort((a, b) => rank(a.kind) - rank(b.kind) || a.id.localeCompare(b.id));
  return { changes };
}

/**
 * The order they are worth looking at in.
 *
 * Removed first: those are the ones holding work that needs a decision. Then
 * changed, because a step whose approver moved is the other thing that alters
 * what a migration means. Added and unchanged are context.
 */
const ORDER: NodeChangeKind[] = ['removed', 'changed', 'added', 'unchanged'];
function rank(kind: NodeChangeKind): number {
  return ORDER.indexOf(kind);
}

function describeDifferences(before: ApiNode, after: ApiNode): string[] {
  const differences: string[] = [];
  for (const field of COMPARED) {
    const was = field.read(before);
    const now = field.read(after);
    if (was === now) continue;
    differences.push(`${field.label}: ${was === '' ? '(none)' : was} → ${now === '' ? '(none)' : now}`);
  }
  return differences;
}

/** The nodes whose work needs somewhere to go, in the order shown. */
export function removedNodes(diff: VersionDiff): NodeChange[] {
  return diff.changes.filter((change) => change.kind === 'removed');
}

/** Everything the new version has, as options for "where should this land". */
export function landingChoices(diff: VersionDiff): ApiNode[] {
  return diff.changes
    .filter((change) => change.after !== undefined)
    .map((change) => change.after as ApiNode)
    .sort((a, b) => a.id.localeCompare(b.id));
}

/**
 * One sentence for the top of the diff.
 *
 * Counts rather than a list: the list is right underneath, and what somebody
 * wants first is whether this is a rename or a rewrite.
 */
export function diffSummary(diff: VersionDiff): string {
  const counted = (kind: NodeChangeKind) => diff.changes.filter((change) => change.kind === kind).length;
  const removed = counted('removed');
  const added = counted('added');
  const changed = counted('changed');

  if (removed === 0 && added === 0 && changed === 0) {
    return 'The two versions have the same steps, configured the same way.';
  }
  const parts: string[] = [];
  if (removed > 0) parts.push(`${removed} step${removed === 1 ? '' : 's'} removed`);
  if (added > 0) parts.push(`${added} added`);
  if (changed > 0) parts.push(`${changed} changed`);
  return `${parts.join(', ')}.`;
}

/**
 * A mapping proposed from the diff, for the rows a person then edits.
 *
 * Only the unambiguous case is proposed: exactly one node removed and exactly
 * one added, which is what a rename looks like from the outside. Guessing
 * between several would be inventing a decision about somebody's work, and a
 * wrong guess pre-filled is worse than an empty row — it is the one nobody
 * re-reads.
 */
export function proposeMapping(diff: VersionDiff): Record<string, string> {
  const removed = diff.changes.filter((change) => change.kind === 'removed');
  const added = diff.changes.filter((change) => change.kind === 'added');
  if (removed.length !== 1 || added.length !== 1) return {};
  return { [removed[0].id]: added[0].id };
}

/**
 * What changes for instances started after a version goes live.
 *
 * The diff says removed/added/changed between two versions. Promoting reads
 * that forward and rolling back reads it in reverse, and the words have to
 * follow: a step the version you are going back to still has is one that
 * *returns*, not one that is new. Getting that backwards in a confirmation is
 * how somebody rolls back believing they are rolling forward.
 *
 * Only what is visible in behaviour is listed. Nothing is said about instances
 * already running, because nothing happens to them — they finish on the version
 * they started on, which the dialog says separately and which is the fact
 * people most often disbelieve.
 */
export function rolloutEffect(diff: VersionDiff, rollingBack: boolean): string[] {
  const lines: string[] = [];
  for (const change of diff.changes) {
    const label = change.before?.name ?? change.after?.name ?? change.id;
    switch (change.kind) {
      case 'removed':
        lines.push(`"${label}" is no longer a step.`);
        break;
      case 'added':
        lines.push(rollingBack ? `"${label}" is a step again.` : `"${label}" is a new step.`);
        break;
      case 'changed':
        lines.push(`"${label}" changes: ${change.differences.join('; ')}.`);
        break;
      default:
        break;
    }
  }
  return lines;
}
