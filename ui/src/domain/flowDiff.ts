import type { ApiFlow } from '../services/types';
import { stepTitle, type VersionIndex } from './stepSettings';

/**
 * What changed about the paths between steps.
 *
 * The version comparison only looked at steps, so a gateway whose condition
 * went from "under 500" to "under 100", or an approval whose way out now leads
 * somewhere else, compared as "the same steps, configured the same way" — and
 * those are the edits that decide which way somebody's quotation goes.
 */

/** How one path differs between the two versions. Unchanged paths are not listed. */
export type FlowChangeKind = 'removed' | 'added' | 'changed';

export interface FlowChange {
  /** The path's id, as the newer version has it when both do. */
  id: string;
  kind: FlowChangeKind;
  before?: ApiFlow;
  after?: ApiFlow;
  /**
   * Where it goes, named by the steps it joins: `from "Approve" to "Pay"`.
   * As the older version has it, for a path it has — that is the one the
   * reader knows, and what it now does differently is in `differences`.
   */
  route: string;
  differences: string[];
}

const ORDER: FlowChangeKind[] = ['removed', 'changed', 'added'];

/**
 * Compares the paths of two versions.
 *
 * Paired by id, and then — for what is left over — by the two steps a path
 * joins. The designer gives an arrow that is drawn again a new id; it goes
 * from the same step to the same step, and no instance could tell the two
 * apart, so calling it one path removed and another added would be two lines
 * about nothing. Map lookups throughout, so a large definition stays linear.
 */
export function diffFlows(before: VersionIndex, after: VersionIndex): FlowChange[] {
  const changes: FlowChange[] = [];
  const unpaired: ApiFlow[] = [];
  for (const [id, flow] of before.flows) {
    const counterpart = after.flows.get(id);
    if (counterpart) addIfChanged(changes, flow, counterpart, before, after);
    else unpaired.push(flow);
  }

  const drawnAgain = byEnds([...after.flows.values()].filter((flow) => !before.flows.has(flow.id)));
  for (const flow of unpaired) {
    const twin = drawnAgain.get(endsOf(flow))?.shift();
    if (twin) addIfChanged(changes, flow, twin, before, after);
    else changes.push({ id: flow.id, kind: 'removed', before: flow, route: routeOf(flow, before), differences: [] });
  }
  for (const flows of drawnAgain.values()) {
    for (const flow of flows) {
      changes.push({ id: flow.id, kind: 'added', after: flow, route: routeOf(flow, after), differences: [] });
    }
  }

  return changes.sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind) || a.id.localeCompare(b.id));
}

function addIfChanged(changes: FlowChange[], was: ApiFlow, now: ApiFlow, before: VersionIndex, after: VersionIndex) {
  const differences: string[] = [];
  if (was.target_ref !== now.target_ref) differences.push(`now leads to ${stepTitle(now.target_ref, after)}`);
  if (was.source_ref !== now.source_ref) differences.push(`now starts at ${stepTitle(now.source_ref, after)}`);
  const condition = (flow: ApiFlow) => (flow.condition ?? '').trim();
  if (condition(was) !== condition(now)) {
    differences.push(`condition: ${condition(was) || '(none)'} → ${condition(now) || '(none)'}`);
  }
  if (differences.length === 0) return;
  changes.push({ id: now.id, kind: 'changed', before: was, after: now, route: routeOf(was, before), differences });
}

function routeOf(flow: ApiFlow, version: VersionIndex): string {
  return `from ${stepTitle(flow.source_ref, version)} to ${stepTitle(flow.target_ref, version)}`;
}

function endsOf(flow: ApiFlow): string {
  return `${flow.source_ref}\u0000${flow.target_ref}`;
}

function byEnds(flows: ApiFlow[]): Map<string, ApiFlow[]> {
  const grouped = new Map<string, ApiFlow[]>();
  for (const flow of flows) {
    const group = grouped.get(endsOf(flow));
    if (group) group.push(flow);
    else grouped.set(endsOf(flow), [flow]);
  }
  return grouped;
}
