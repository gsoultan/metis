/**
 * A project's archive: the finished instances the retention sweep moved out of
 * the live tables, read-only.
 *
 * Read from `GET /api/v1/projects/{id}/archived-instances`, most recently
 * finished first, a page at a time behind the server's cursor, and one record
 * from `GET /api/v1/archived-instances/{id}`. What reaches the query string and
 * how a record's trail becomes the timeline's entries are decidable without a
 * network, so they live here, tested.
 */
import type { ApiAuditEntry } from '../services/types';

/** How many archived instances a page asks for. */
export const ARCHIVE_PAGE_SIZE = 50;

/** One archived instance as the list shows it. */
export interface ArchivedInstance {
  id: string;
  project_id: string;
  definition_id: string;
  process_key: string;
  process_name: string;
  process_version: number;
  business_key?: string;
  status: string;
  started_at: string;
  finished_at: string;
  archived_at: string;
}

/** One page, and unless it was the last, the cursor for the next. */
export interface ArchivePage {
  instances: ArchivedInstance[];
  next_cursor?: string;
}

/** What the list is narrowed to. An empty field is not sent. */
export interface ArchiveFilters {
  processKey?: string;
  businessKey?: string;
}

/** An archived audit entry, as the record keeps it. */
export interface ArchivedAuditEntry {
  id: string;
  created_at: string;
  type: string;
  message: string;
  narrative?: string;
  node_id?: string;
  node_name?: string;
  actor?: string;
  data?: Record<string, unknown>;
}

export interface ArchivedTask {
  id: string;
  created_at: string;
  updated_at: string;
  node_id: string;
  name: string;
  status: string;
  assignee?: string;
}

export interface ArchivedIncident {
  id: string;
  created_at: string;
  node_id: string;
  error: string;
  status: string;
  resolved_at?: string;
}

export interface ArchivedServiceCall {
  id: string;
  node_id: string;
  status: string;
  attempts: number;
  completed_at?: string;
}

/** Everything kept of an archived instance. */
export interface ArchiveRecord {
  instance: { id: string; status: string; business_key?: string; variables?: Record<string, unknown> };
  tasks?: ArchivedTask[] | null;
  service_calls?: ArchivedServiceCall[] | null;
  incidents?: ArchivedIncident[] | null;
  audit_trail?: ArchivedAuditEntry[] | null;
}

export interface OpenedArchive {
  instance: ArchivedInstance;
  record: ArchiveRecord;
}

/** The query string for one page; the cursor is the page before's `next_cursor`. */
export function archiveQueryString(filters: ArchiveFilters, cursor?: string, limit = ARCHIVE_PAGE_SIZE): string {
  const params = new URLSearchParams();
  const processKey = filters.processKey?.trim();
  const businessKey = filters.businessKey?.trim();
  if (processKey) params.set('process_key', processKey);
  if (businessKey) params.set('business_key', businessKey);
  if (cursor) params.set('cursor', cursor);
  params.set('limit', String(limit));
  return `?${params.toString()}`;
}

/** The archived trail as the timeline reads a live one, oldest first. */
export function timelineEntriesOf(trail: ArchivedAuditEntry[] | null | undefined): ApiAuditEntry[] {
  return (trail ?? []).map((entry) => ({
    id: entry.id,
    type: entry.type,
    message: entry.message,
    narrative: entry.narrative,
    timestamp: entry.created_at,
    node: entry.node_id ? { id: entry.node_id, name: entry.node_name } : undefined,
    data: entry.data,
  }));
}

/** The process an archived instance ran, as a person names it. */
export function processLabel(instance: Pick<ArchivedInstance, 'process_name' | 'process_key' | 'process_version'>): string {
  const name = instance.process_name || instance.process_key;
  return `${name} v${instance.process_version}`;
}

/** A value as one line of text: as it is when it is plain, written out when it is a list or an object. */
export function valueText(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}
