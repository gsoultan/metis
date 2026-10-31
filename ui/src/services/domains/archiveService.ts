/**
 * A project's archive of finished instances, read-only, over REST.
 */
import { archiveQueryString, type ArchiveFilters, type ArchivePage, type OpenedArchive } from '../../domain/archive';
import { requestJSON } from '../shared/rest';

export const archiveService = {
  /** One page, most recently finished first; `cursor` is the page before's `next_cursor`. */
  async list(projectId: string, filters: ArchiveFilters, cursor?: string, signal?: AbortSignal): Promise<ArchivePage> {
    const reply = await requestJSON<Partial<ArchivePage>>(
      `/projects/${encodeURIComponent(projectId)}/archived-instances${archiveQueryString(filters, cursor)}`,
      { signal },
    );
    return { instances: reply.instances ?? [], next_cursor: reply.next_cursor || undefined };
  },

  /** One archived instance and everything kept of it. */
  async get(instanceId: string, signal?: AbortSignal): Promise<OpenedArchive> {
    return requestJSON<OpenedArchive>(`/archived-instances/${encodeURIComponent(instanceId)}`, { signal });
  },
};
