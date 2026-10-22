import { infiniteQueryOptions, useInfiniteQuery } from '@tanstack/react-query';

import {
  AUDIT_PAGE_SIZE,
  nextAuditCursor,
  operationsOf,
  type AuditFilters,
} from '../domain/auditOperations';
import { auditService } from '../services/domains/auditService';
import { useAppStore } from '../store/useAppStore';

/**
 * The organization is in the key, as are the filters: changing either starts
 * the list again from the newest operation rather than appending a different
 * question's answers to the old one's pages.
 */
export const auditKeys = {
  all: ['audit-operations'] as const,
  pages: (organizationId: string | null, filters: AuditFilters) =>
    ['audit-operations', organizationId, filters] as const,
};

export function auditOperationsQuery(organizationId: string | null, filters: AuditFilters) {
  return infiniteQueryOptions({
    queryKey: auditKeys.pages(organizationId, filters),
    queryFn: ({ pageParam, signal }) =>
      auditService.listOperations(filters, pageParam, AUDIT_PAGE_SIZE, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => nextAuditCursor(lastPage),
  });
}

/** Who did what across the organization being worked in, newest first, a page at a time. */
export function useAuditOperations(filters: AuditFilters) {
  const organizationId = useAppStore((state) => state.currentOrganizationId);
  const token = useAppStore((state) => state.token);
  const pages = useInfiniteQuery({
    ...auditOperationsQuery(organizationId, filters),
    enabled: !!token,
  });

  return {
    operations: operationsOf(pages.data?.pages ?? []),
    isLoading: pages.isLoading,
    isFetching: pages.isFetching,
    error: pages.error,
    refetch: () => {
      void pages.refetch();
    },
    hasMore: pages.hasNextPage,
    isLoadingMore: pages.isFetchingNextPage,
    loadMore: () => {
      void pages.fetchNextPage();
    },
  };
}
