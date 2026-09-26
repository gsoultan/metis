import {
  infiniteQueryOptions,
  queryOptions,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';

import { nextNotificationPage, notificationsOf } from '../domain/notificationPages';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';

// How often the bell asks for its count. A minute is often enough for a
// number that says "look at me", and all a poll sends back is that number.
const NOTIFICATION_POLL_INTERVAL_MS = 60_000;

/** A screenful: what the list shows first, and how many older ones each "load older" adds. */
export const NOTIFICATION_PAGE_SIZE = 20;

/**
 * Everything under `all` is the signed-in person's notifications, so reading
 * or clearing one invalidates the count and the pages together. The user id is
 * in the keys so one person's cache is never read as the next person's.
 */
export const notificationKeys = {
  all: ['notifications'] as const,
  unreadCount: (userId: string | undefined) => ['notifications', 'unread-count', userId] as const,
  pages: (userId: string | undefined) => ['notifications', 'pages', userId] as const,
};

/** The bell's number: how many of the signed-in person's notifications are unread. */
export function unreadCountQuery(userId: string | undefined) {
  return queryOptions({
    queryKey: notificationKeys.unreadCount(userId),
    queryFn: ({ signal }) => processService.countUnreadNotifications(signal),
  });
}

/** The signed-in person's notifications, newest first, a page at a time. */
export function notificationPagesQuery(userId: string | undefined) {
  return infiniteQueryOptions({
    queryKey: notificationKeys.pages(userId),
    queryFn: ({ pageParam, signal }) =>
      processService.listOwnNotifications(pageParam, NOTIFICATION_PAGE_SIZE, signal),
    initialPageParam: 1,
    getNextPageParam: (lastPage, _pages, lastPageParam) => nextNotificationPage(lastPage, lastPageParam),
  });
}

/**
 * The notification bell and the list behind it.
 *
 * Only the count is polled. The list is read while it is open — `listOpen` —
 * one page at a time, older pages when asked for; before this, every poll sent
 * the newest thousand notifications whether or not anybody looked, and the
 * bell's number was counted among those, so an older unread one was never
 * counted.
 */
export const useNotifications = (listOpen: boolean) => {
  const user = useAppStore((state) => state.user);
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: notificationKeys.all });

  const unread = useQuery({
    ...unreadCountQuery(user?.id),
    enabled: !!user,
    refetchInterval: NOTIFICATION_POLL_INTERVAL_MS,
  });

  const pages = useInfiniteQuery({
    ...notificationPagesQuery(user?.id),
    enabled: !!user && listOpen,
  });

  const markAsRead = useMutation({
    mutationFn: (id: string) => processService.markAsRead(id),
    onSuccess: refresh,
  });

  const markAllAsRead = useMutation({
    mutationFn: () => user ? processService.markAllAsRead(user.username) : Promise.resolve({}),
    onSuccess: refresh,
  });

  const deleteNotification = useMutation({
    mutationFn: (id: string) => processService.deleteNotification(id),
    onSuccess: refresh,
  });

  return {
    unreadCount: unread.data ?? 0,
    notifications: notificationsOf(pages.data?.pages.map((page) => page.notifications) ?? []),
    isLoading: pages.isLoading,
    failed: pages.isError,
    hasOlder: pages.hasNextPage,
    isLoadingOlder: pages.isFetchingNextPage,
    loadOlder: () => {
      void pages.fetchNextPage();
    },
    markAsRead,
    markAllAsRead,
    deleteNotification,
  };
};
