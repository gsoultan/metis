import { splitRoles } from './roles';

/**
 * What the browser knows of somebody's roles.
 *
 * `roles` is the whole set, as the login reply lists it. `role` is the same
 * set joined into one string for display, and the only one a session saved
 * before the store kept the list has — so it is read when the list is absent,
 * and never added to it.
 */
export interface RoleHolder {
  role?: string;
  roles?: readonly string[];
}

/**
 * Whether the user holds the role, for deciding what to show them.
 *
 * The pages asked `user.role === 'ADMIN'`. That string holds every role joined,
 * so an administrator who was anything else as well was not shown what the
 * server would have let them do. Case is ignored because the server ignores it
 * and accounts written by the older role picker hold "admin".
 *
 * This decides what is shown, never what is allowed: the server checks again.
 */
export function hasRole(user: RoleHolder | null | undefined, role: string): boolean {
  const wanted = role.trim().toLowerCase();
  if (!user || wanted === '') {
    return false;
  }
  const held = user.roles ?? splitRoles(user.role ?? '');
  return held.some((candidate) => candidate.trim().toLowerCase() === wanted);
}
