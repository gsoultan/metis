import { afterEach, describe, expect, test } from "bun:test";
import { InfiniteQueryObserver, QueryClient } from "@tanstack/react-query";

import { stubFetchAnswering } from "../services/shared/stubbedFetch";
import { NOTIFICATION_PAGE_SIZE, notificationPagesQuery } from "./useNotification";

/**
 * The notification list reads a page at a time, and asks for the next page
 * when somebody asks for older notifications — rather than being sent the
 * newest thousand on every poll.
 */
describe("the notification list", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("asks for the next page each time older ones are wanted, and none past the last", async () => {
    const stub = stubFetchAnswering((url) => {
      const page = Number(url.searchParams.get("page"));
      return {
        notifications: [{ id: `n-${page}`, type: "System", title: "", message: "", is_read: true, created_at: "" }],
        page: { total: 41, page, page_size: NOTIFICATION_PAGE_SIZE, has_more: page < 3 },
      };
    });
    restore = stub.restore;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const pages = new InfiniteQueryObserver(client, notificationPagesQuery("u-1"));

    await pages.refetch();
    await pages.fetchNextPage();
    await pages.fetchNextPage();
    const last = await pages.fetchNextPage();

    const asked = stub.sent.map((request) => new URL(request.url, "http://stub.invalid").search);
    expect(asked).toEqual([
      `?page=1&page_size=${NOTIFICATION_PAGE_SIZE}`,
      `?page=2&page_size=${NOTIFICATION_PAGE_SIZE}`,
      `?page=3&page_size=${NOTIFICATION_PAGE_SIZE}`,
    ]);
    expect(last.hasNextPage).toBe(false);
    expect(last.data?.pages.map((page) => page.notifications[0].id)).toEqual(["n-1", "n-2", "n-3"]);
  });
});
