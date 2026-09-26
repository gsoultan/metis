/**
 * Granting or revoking one role, from the matrix on the Platform access page.
 *
 * A change goes through the same user update the Accounts view's edit dialog
 * uses, PUT /users/{id}. That update writes the name, display name and email it
 * is sent — an empty one included, because clearing a field is an edit — so a
 * change that sent only the roles would blank all three. Every change here
 * carries the account's other fields as the list holds them.
 *
 * A cell reads the account from the list, never from what was clicked. A
 * refused change writes nothing, so its checkbox stays as it was; an accepted
 * one writes the roles the server now holds into the list.
 */
import { errorMessage } from '../services/shared/errors';
import type { UserUpdate } from '../services/domains/identityService';
import type { ApiOrganizationUser } from '../services/types';
import { isPrivilegedRole } from './roles';

/** One role change, as the user update takes it. */
export type RoleUpdate = { id: string; roles: string[] } & UserUpdate;

/** An account list as the Accounts view and the matrix hold it. */
export interface AccountList {
  users: ApiOrganizationUser[];
}

const same = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/**
 * The roles with one granted or revoked, and the rest kept.
 *
 * Revoking takes it away in whatever case it was written — an older account
 * can hold "admin" — and granting adds it as the server spells it. A role the
 * matrix has no column for stays: taking away something nobody clicked is not
 * a side effect a checkbox may have.
 */
export function rolesWith(roles: readonly string[] | undefined, role: string, granted: boolean): string[] {
  const kept = (roles ?? []).filter((held) => !same(held, role));
  return granted ? [...kept, role] : kept;
}

/** The update that grants or revokes one role and leaves the rest of the account as it is. */
export function roleUpdate(account: ApiOrganizationUser, role: string, granted: boolean): RoleUpdate {
  return {
    id: account.id,
    full_name: account.full_name ?? '',
    display_name: account.display_name ?? '',
    email: account.email ?? '',
    roles: rolesWith(account.roles, role, granted),
  };
}

/**
 * An account list with one account's roles as the server now holds them.
 *
 * The update stores the roles it is sent, so once it is accepted they are the
 * account's roles, and the next change to the same person has to start from
 * them. Waiting for the list to be read again is not enough: a second change
 * elsewhere cancels that read and starts another, and a click in between would
 * be worked out from the list as it was — sending the roles without the first
 * change, and taking it away again.
 */
export function withAccountRoles<T extends AccountList>(list: T | undefined, accountId: string, roles: string[]): T | undefined {
  if (!list) return list;
  return { ...list, users: list.users.map((account) => (account.id === accountId ? { ...account, roles } : account)) };
}

/**
 * Whether a change takes the administrator role from the person making it.
 *
 * One click in the matrix does what used to take opening a dialog and pressing
 * Update, and only another administrator can undo it — so it is asked about
 * first. The server still refuses it outright if nobody else administers the
 * organization.
 */
export function revokesOwnAdministration(
  account: ApiOrganizationUser,
  role: string,
  granted: boolean,
  currentUserId: string,
): boolean {
  return !granted && account.id === currentUserId && isPrivilegedRole(role);
}

/** How a change went: made, or refused in the server's own words. */
export type RoleChangeOutcome = { changed: true } | { changed: false; reason: string };

/**
 * Sends one change. A refusal is an answer rather than an exception: the
 * server's words come back to be shown to the person who asked.
 */
export async function sendRoleChange(
  update: RoleUpdate,
  save: (update: RoleUpdate) => Promise<unknown>,
): Promise<RoleChangeOutcome> {
  try {
    await save(update);
    return { changed: true };
  } catch (error: unknown) {
    return { changed: false, reason: errorMessage(error, 'The server gave no reason') };
  }
}

/** What the person is told about one change, in the shape a notification takes. */
export type RoleChangeNotice = {
  title: string;
  message: string;
  color: 'green' | 'red';
};

type Translate = (key: string, values?: Record<string, string | number>) => string;

/**
 * The notification for one change. A refusal's message is the server's own
 * sentence — "forbidden: ana is the last administrator of Acme; make somebody
 * else an administrator first" — which already says what to do about it.
 */
export function roleChangeNotice(
  outcome: RoleChangeOutcome,
  change: { name: string; role: string; granted: boolean },
  t: Translate,
): RoleChangeNotice {
  if (!outcome.changed) {
    return { title: t('access.refusedTitle', { name: change.name }), message: outcome.reason, color: 'red' };
  }
  const message = change.granted ? 'access.granted' : 'access.revoked';
  return { title: t('access.savedTitle'), message: t(message, { name: change.name, role: change.role }), color: 'green' };
}
