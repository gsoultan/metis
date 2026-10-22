/**
 * The organization's audit log of user operations: who did what, newest first.
 *
 * Read from `GET /api/v1/audit/operations`, a page at a time behind an opaque
 * keyset cursor. Everything here is decidable without a network — which
 * filters reach the query string, how a day picked on screen becomes the
 * half-open range the server reads, and what an event type is called — so it
 * lives here, tested, and the page only draws it.
 */

/** One operation as the server sends it. Optional fields are omitted on the wire when there is none. */
export interface AuditOperation {
  id: string;
  /** RFC 3339, UTC. */
  at: string;
  actor: string;
  type: string;
  project_id: string;
  instance_id?: string;
  node_id?: string;
  node_name?: string;
  /** The server's sentence for what happened, already in words. */
  narrative: string;
}

/** One page: the operations and, unless this was the last page, the cursor for the next. */
export interface AuditOperationsPage {
  operations: AuditOperation[];
  next?: string;
}

/** What the list is narrowed to. Every field is optional; an empty one is not sent. */
export interface AuditFilters {
  actor?: string;
  type?: string;
  project?: string;
  /** RFC 3339, inclusive. */
  from?: string;
  /** RFC 3339, exclusive. */
  to?: string;
}

/** The server's default page size, asked for explicitly so the page does not depend on it. */
export const AUDIT_PAGE_SIZE = 50;

/** The server refuses a limit outside 1..200. */
const MAX_LIMIT = 200;

/**
 * The event types offered in the type filter, in the order people look for
 * them: task work, then instance interventions, then deviations, then
 * retention. A type the server sends that is not listed here still shows, by
 * its raw name; it simply cannot be filtered to from the select.
 */
export const AUDIT_EVENT_TYPES = [
  'task_claimed',
  'task_unclaimed',
  'task_completed',
  'task_assigned',
  'task_delegated',
  'task_resolved',
  'task_edited',
  'task_escalated',
  'instance_migrated',
  'node_skipped',
  'instance_cancelled',
  'instance_held',
  'step_activated',
  'deviation_requested',
  'deviation_approved',
  'deviation_self_approved',
  'deviation_rejected',
  'deviation_stale',
  'retention_hold_placed',
  'retention_hold_released',
] as const;

/**
 * The query string for one page: the filters that carry a value, the cursor
 * when there is one, and the page size clamped to what the server accepts.
 * Leading `?` included, or empty when there is nothing to send.
 */
export function auditQueryString(filters: AuditFilters, before?: string, limit = AUDIT_PAGE_SIZE): string {
  const params = new URLSearchParams();
  const put = (name: string, value: string | undefined) => {
    const trimmed = value?.trim();
    if (trimmed) params.set(name, trimmed);
  };
  put('actor', filters.actor);
  put('type', filters.type);
  put('project', filters.project);
  put('from', filters.from);
  put('to', filters.to);
  put('before', before);
  params.set('limit', String(Math.min(MAX_LIMIT, Math.max(1, Math.trunc(limit)))));
  return `?${params.toString()}`;
}

/** The cursor for the page after `page`, or undefined on the last one — the shape TanStack's infinite query wants. */
export function nextAuditCursor(page: AuditOperationsPage): string | undefined {
  return page.next ? page.next : undefined;
}

/** Every operation on the pages read so far, in the order the server sent them. */
export function operationsOf(pages: readonly AuditOperationsPage[]): AuditOperation[] {
  return pages.flatMap((page) => page.operations ?? []);
}

/**
 * A day picked on screen (`YYYY-MM-DD`, the person's local calendar) as the
 * instant it starts, in RFC 3339. `undefined` for nothing picked or a string
 * that is not a date.
 */
export function startOfLocalDay(day: string | null | undefined): string | undefined {
  if (!day) return undefined;
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(day);
  if (!match) return undefined;
  const start = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  return Number.isNaN(start.getTime()) ? undefined : start.toISOString();
}

/**
 * The exclusive upper bound for an inclusive "to" day: the instant the next
 * local day starts. Somebody who picks 22 October as the end means the whole
 * of the 22nd, and the server's `to` excludes the instant it is given.
 */
export function endOfLocalDay(day: string | null | undefined): string | undefined {
  const start = startOfLocalDay(day);
  if (!start) return undefined;
  const next = new Date(start);
  next.setDate(next.getDate() + 1);
  return next.toISOString();
}

/**
 * What an event type is called. The raw type when the catalogue has no words
 * for it — a newer server's type is still readable, where the bare message key
 * would not be.
 */
export function auditTypeLabel(type: string, t: (key: string) => string): string {
  const key = `audit.type.${type}`;
  const label = t(key);
  return label === key ? type : label;
}

/** The first block of a UUID: enough to tell rows apart, short enough for a column. */
export function shortId(id: string): string {
  const head = id.split('-')[0];
  return head && head.length > 0 ? head : id;
}
