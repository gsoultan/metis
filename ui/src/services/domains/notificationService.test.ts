import { afterEach, describe, expect, test } from "bun:test";

import { API_BASE_URL } from "../shared/config";
import { stubFetch } from "../shared/stubbedFetch";
import { notificationService } from "./notificationService";

/**
 * The bell asks the server two things about whoever is signed in: how many of
 * their notifications are unread, and one page of them. Neither names the
 * person — the server takes that from the session.
 */
describe("notificationService", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks the server how many of the signed-in person's notifications are unread", async () => {
    const stub = stubFetch({ unread_count: 1050 });
    restore = stub.restore;

    expect(await notificationService.countUnreadNotifications()).toBe(1050);
    expect(stub.sent[0].method).toBe("GET");
    expect(stub.sent[0].url).toBe(`${API_BASE_URL}/users/me/notifications/unread-count`);
  });

  test("asks for one page of notifications, and reads where it sits among them", async () => {
    const stub = stubFetch({
      notifications: [
        { id: "n-21", type: "TaskAssignment", title: "Approve", message: "", is_read: false, created_at: "2026-09-26T10:00:00Z" },
      ],
      page: { total: 45, page: 2, page_size: 20, has_more: true },
    });
    restore = stub.restore;

    const page = await notificationService.listOwnNotifications(2, 20);

    expect(stub.sent[0].method).toBe("GET");
    expect(stub.sent[0].url).toBe(`${API_BASE_URL}/users/me/notifications?page=2&page_size=20`);
    expect(page.notifications.map((n) => n.id)).toEqual(["n-21"]);
    expect(page).toMatchObject({ page: 2, pageSize: 20, total: 45, hasMore: true });
  });

  test("reads an empty page as no notifications and nothing older", async () => {
    ({ restore } = stubFetch({ notifications: null, page: { total: 0, page: 1, page_size: 20, has_more: false } }));

    expect(await notificationService.listOwnNotifications(1, 20)).toEqual({
      notifications: [],
      page: 1,
      pageSize: 20,
      total: 0,
      hasMore: false,
    });
  });

  test("raises a refusal rather than answering with nothing", async () => {
    ({ restore } = stubFetch({ error: "unauthorized" }, 401));

    await expect(notificationService.countUnreadNotifications()).rejects.toThrow("unauthorized");
    await expect(notificationService.listOwnNotifications(1, 20)).rejects.toThrow("unauthorized");
  });
});
