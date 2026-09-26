import { raiseIfRefused } from "../raise";
import { requestJSON } from "../shared/rest";

export type Notification = {
  id: string;
  user_id: string;
  type: string;
  title: string;
  message: string;
  is_read: boolean;
  link?: string;
  created_at: string;
  project_id?: string;
  instance_id?: string;
};

/** One page of the signed-in person's notifications, newest first. */
export type NotificationPage = {
  notifications: Notification[];
  page: number;
  pageSize: number;
  /** How many notifications the person has in all. */
  total: number;
  /** Whether there are older ones than this page. */
  hasMore: boolean;
};

type PageResponse = { total: number; page: number; page_size: number; has_more: boolean };

type OwnNotificationsResponse = {
  notifications?: Notification[] | null;
  page?: PageResponse;
  err?: string;
};

type UnreadCountResponse = {
  unread_count?: number;
  err?: string;
};

type GenericResponse = {
  error?: string;
};

/**
 * The notification centre's reads are the signed-in person's own: the server
 * takes whose they are from the session, so nothing here names anybody.
 */
export const notificationService = {
  /** How many of the signed-in person's notifications are unread — all of them, counted on the server. */
  async countUnreadNotifications(signal?: AbortSignal): Promise<number> {
    const data = raiseIfRefused(
      await requestJSON<UnreadCountResponse>("/users/me/notifications/unread-count", { signal }),
    );
    return data.unread_count ?? 0;
  },

  /** One page of the signed-in person's notifications, newest first. */
  async listOwnNotifications(page: number, pageSize: number, signal?: AbortSignal): Promise<NotificationPage> {
    const query = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    const data = raiseIfRefused(
      await requestJSON<OwnNotificationsResponse>(`/users/me/notifications?${query}`, { signal }),
    );
    return {
      notifications: data.notifications ?? [],
      page: data.page?.page ?? page,
      pageSize: data.page?.page_size ?? pageSize,
      total: data.page?.total ?? 0,
      hasMore: data.page?.has_more ?? false,
    };
  },

  async markAsRead(id: string, signal?: AbortSignal) {
    return await requestJSON<GenericResponse>(`/notifications/${id}/read`, {
      method: "POST",
      signal,
    });
  },

  async markAllAsRead(userId: string, signal?: AbortSignal) {
    return await requestJSON<GenericResponse>(`/notifications/read-all?user_id=${userId}`, {
      method: "POST",
      signal,
    });
  },

  async deleteNotification(id: string, signal?: AbortSignal) {
    return await requestJSON<GenericResponse>(`/notifications/${id}`, {
      method: "DELETE",
      signal,
    });
  },
};
