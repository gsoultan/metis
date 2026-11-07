import type { ApprovalBehind, ApprovalRequest, ApprovalTakeover } from '../services/domains/approvalLineService';

/** One request on an older line, as the review table shows it. */
export interface BehindRow {
  id: string;
  version: number;
  title: string;
  submittedBy: string;
  /** Where it is: the step it waits at, or that it was sent back. */
  where: string;
  /** Who it waits on now, on the line it was submitted on. */
  waitsFor: string;
  /** Why it cannot be moved from here, or null when it can. */
  blocked: string | null;
}

/** The names a step or task is offered to, people first. */
function names(users?: string[], groups?: string[]): string {
  const all = [...(users ?? []), ...(groups ?? []).map((g) => `${g} (group)`)];
  return all.length ? all.join(', ') : 'nobody';
}

/** Why a version's requests cannot be moved here, or null. */
export function versionBlocked(behind: ApprovalBehind): string | null {
  const plan = behind.plan ?? {};
  if (plan.refusals?.length) return plan.refusals[0];
  if (plan.compliance_holds?.length) return 'Moving them would take an approval away. Use Move work in the version list.';
  if (plan.requires_second_approver) return 'Moving them needs a second administrator. Use Move work in the version list.';
  return null;
}

function rowOf(request: ApprovalRequest, version: number, blocked: string | null): BehindRow {
  const title = typeof request.variables?.title === 'string' && request.variables.title ? request.variables.title : request.instance_id.slice(0, 8);
  const waiting = request.steps.find((s) => s.status === 'waiting');
  let where = 'Running';
  let waitsFor = '—';
  if (request.revision) {
    where = 'Sent back to revise';
    waitsFor = request.revision.assignee || request.submitted_by || 'the submitter';
  } else if (waiting) {
    where = waiting.name;
    waitsFor = waiting.waiting?.assignee || names(waiting.waiting?.users ?? waiting.users, waiting.waiting?.groups ?? waiting.groups);
  }
  return { id: request.instance_id, version, title, submittedBy: request.submitted_by || '—', where, waitsFor, blocked };
}

/** Every request on an older line, newest version first, oldest request first. */
export function behindRows(takeover: ApprovalTakeover | undefined): BehindRow[] {
  if (!takeover) return [];
  return takeover.behind.flatMap((b) => {
    const blocked = versionBlocked(b);
    return b.requests.map((r) => rowOf(r, b.version, blocked));
  });
}

/** The selected rows that can be moved. */
export function movable(rows: BehindRow[], selected: ReadonlySet<string>): string[] {
  return rows.filter((r) => selected.has(r.id) && !r.blocked).map((r) => r.id);
}

/** What to tell the person after a move. */
export function moveSummary(moved: number, left: number, live: number): string {
  const what = moved === 1 ? '1 request' : `${moved} requests`;
  const head = moved > 0 ? `Moved ${what} to v${live}. Their open step goes to the new line's approvers.` : 'Nothing was moved.';
  if (left === 0) return head;
  return `${head} ${left === 1 ? '1 request was left on its line' : `${left} requests were left on their line`}, with the reason.`;
}
