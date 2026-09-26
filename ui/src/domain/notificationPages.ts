/**
 * Reading a person's notifications a page at a time.
 *
 * The list used to be sent the newest thousand on every poll: too much to send
 * each minute, and too little for anybody with more, whose older notifications
 * could not be reached at all. It reads a page now, newest first, and the next
 * one when somebody asks for older notifications.
 */

/** As much of a page as deciding what to ask for next needs. */
export interface PageShape {
  /** Whether the server has older notifications than this page. */
  hasMore: boolean;
}

/**
 * The number of the page after `lastPage`, or undefined when the server said
 * there is nothing older. Counted from the page that was asked for rather than
 * from the page number the server echoes, so the next request never depends
 * on the reply agreeing.
 */
export function nextNotificationPage(last: PageShape, lastPage: number): number | undefined {
  return last.hasMore ? lastPage + 1 : undefined;
}

/**
 * Every page read so far as one list, newest first, each notification once.
 *
 * Pages are windows by position, so a notification arriving between two reads
 * pushes the last one of a page onto the next, and it comes back twice. The
 * first time it was seen is where it stays.
 */
export function notificationsOf<T extends { id: string }>(pages: T[][]): T[] {
  const seen = new Set<string>();
  const notifications: T[] = [];
  for (const notification of pages.flat()) {
    if (seen.has(notification.id)) continue;
    seen.add(notification.id);
    notifications.push(notification);
  }
  return notifications;
}
