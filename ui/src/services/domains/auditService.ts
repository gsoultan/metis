/**
 * The organization's audit log of user operations.
 *
 * Over REST rather than Connect: the endpoint is the public one the server
 * documents for exporting the log, so the page reads what an auditor's script
 * would. Errors come back the way every other REST endpoint's do, through
 * requestJSON.
 */
import { auditQueryString, type AuditFilters, type AuditOperationsPage } from '../../domain/auditOperations';
import { requestJSON } from '../shared/rest';

export const auditService = {
  /** One page of operations, newest first; `before` is the previous page's `next`. */
  async listOperations(
    filters: AuditFilters,
    before?: string,
    limit?: number,
    signal?: AbortSignal,
  ): Promise<AuditOperationsPage> {
    const reply = await requestJSON<Partial<AuditOperationsPage>>(
      `/audit/operations${auditQueryString(filters, before, limit)}`,
      { signal },
    );
    return { operations: reply.operations ?? [], next: reply.next || undefined };
  },
};
