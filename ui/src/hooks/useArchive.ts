import { infiniteQueryOptions, useInfiniteQuery, useQuery } from '@tanstack/react-query';

import type { ArchiveFilters } from '../domain/archive';
import { archiveService } from '../services/domains/archiveService';
import { useAppStore } from '../store/useAppStore';

/** The project and the filters are in the key: changing either starts the list again from the newest. */
export const archiveKeys = {
  pages: (projectId: string | null, filters: ArchiveFilters) => ['archive', projectId, filters] as const,
  one: (instanceId: string | null) => ['archive-record', instanceId] as const,
};

export function archivePagesQuery(projectId: string, filters: ArchiveFilters) {
  return infiniteQueryOptions({
    queryKey: archiveKeys.pages(projectId, filters),
    queryFn: ({ pageParam, signal }) => archiveService.list(projectId, filters, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  });
}

/** The current project's archive, most recently finished first, a page at a time. */
export function useArchive(filters: ArchiveFilters) {
  const projectId = useAppStore((state) => state.currentProjectId);
  const token = useAppStore((state) => state.token);
  const pages = useInfiniteQuery({
    ...archivePagesQuery(projectId ?? '', filters),
    enabled: !!token && !!projectId,
  });
  return {
    projectId,
    instances: (pages.data?.pages ?? []).flatMap((page) => page.instances),
    isLoading: pages.isLoading,
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

/** One archived instance's record, read when it is opened. An archive does not change, so it is kept. */
export function useArchivedInstance(instanceId: string | null) {
  return useQuery({
    queryKey: archiveKeys.one(instanceId),
    queryFn: ({ signal }) => archiveService.get(instanceId ?? '', signal),
    enabled: !!instanceId,
    staleTime: Infinity,
  });
}
