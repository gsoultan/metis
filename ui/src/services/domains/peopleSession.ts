import type { PeopleSession } from "./peopleService";

/*
 * Kept in this tab's session storage, under a key of its own, and never in the
 * app store: a People session is not a User's, and closing the tab ends it.
 */
const KEY = "metis.people.session";

function storage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch {
    return null;
  }
}

/** The session this tab holds, or null when there is none or it has run out. */
export function readPeopleSession(now: Date = new Date()): PeopleSession | null {
  let raw: string | null = null;
  try {
    raw = storage()?.getItem(KEY) ?? null;
  } catch {
    return null;
  }
  if (!raw) {
    return null;
  }
  try {
    const session = JSON.parse(raw) as PeopleSession;
    if (!session?.token || !session.person?.username) {
      return null;
    }
    if (session.expires_at && Date.parse(session.expires_at) <= now.getTime()) {
      clearPeopleSession();
      return null;
    }
    return session;
  } catch {
    clearPeopleSession();
    return null;
  }
}

export function savePeopleSession(session: PeopleSession): void {
  try {
    storage()?.setItem(KEY, JSON.stringify(session));
  } catch {
    // A tab that cannot store keeps the session in memory for as long as the
    // page is open; the inbox holds it in state as well.
  }
}

export function clearPeopleSession(): void {
  try {
    storage()?.removeItem(KEY);
  } catch {
    // Nothing stored, nothing to clear.
  }
}

/** The project a sign-in address names: `/p/login?project=<id>`. */
export function projectFromSearch(search: string): string {
  return new URLSearchParams(search).get("project")?.trim() ?? "";
}
