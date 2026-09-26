/**
 * What the notification bell shows and says.
 *
 * The count is the server's, over every notification the person has, so it is
 * no longer bounded by the thousand the list used to be sent. The badge has
 * room for two digits; the bell's name carries the whole number, for anybody
 * who cannot see the badge at all.
 */

/** The largest count the badge shows as it is; past it, the badge says so. */
const BADGE_LIMIT = 99;

const BELL_NAME = 'Notifications';

/** The badge's text, or undefined for no badge when nothing is unread. */
export function bellBadge(unread: number): string | undefined {
  if (unread <= 0) return undefined;
  return unread > BADGE_LIMIT ? `${BADGE_LIMIT}+` : String(unread);
}

/** The bell's accessible name, with the full count when there is one. */
export function bellName(unread: number): string {
  return unread > 0 ? `${BELL_NAME}, ${unread} unread` : BELL_NAME;
}
