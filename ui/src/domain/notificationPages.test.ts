import { describe, expect, test } from "bun:test";

import { nextNotificationPage, notificationsOf } from "./notificationPages";

describe("the notification list's pages", () => {
  test("asks for the page after the last one while the server says there are older ones", () => {
    expect(nextNotificationPage({ hasMore: true }, 1)).toBe(2);
    expect(nextNotificationPage({ hasMore: true }, 7)).toBe(8);
  });

  test("asks for nothing once the server says there is nothing older", () => {
    expect(nextNotificationPage({ hasMore: false }, 3)).toBeUndefined();
  });

  test("shows each notification once when one slides onto the next page", () => {
    // A notification arriving between two reads pushes the last of the first
    // page onto the second, so it comes back twice.
    const first = [{ id: "d" }, { id: "c" }];
    const second = [{ id: "c" }, { id: "b" }, { id: "a" }];

    expect(notificationsOf([first, second]).map((n) => n.id)).toEqual(["d", "c", "b", "a"]);
  });

  test("shows nothing before a page has been read", () => {
    expect(notificationsOf([])).toEqual([]);
  });
});
