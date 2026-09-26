import { describe, expect, test } from "bun:test";

import { bellBadge, bellName } from "./notificationBell";

describe("the notification bell", () => {
  test("shows no badge when nothing is unread", () => {
    expect(bellBadge(0)).toBeUndefined();
  });

  test("shows the count on the badge, and shortens one too wide for it", () => {
    expect(bellBadge(1)).toBe("1");
    expect(bellBadge(99)).toBe("99");
    expect(bellBadge(100)).toBe("99+");
    expect(bellBadge(1050)).toBe("99+");
  });

  test("tells a screen reader the whole count, which the badge cannot show", () => {
    expect(bellName(0)).toBe("Notifications");
    expect(bellName(1)).toBe("Notifications, 1 unread");
    expect(bellName(1050)).toBe("Notifications, 1050 unread");
  });
});
