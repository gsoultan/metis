import type { ApiUser } from '../services/types';

/** The user shape held in the app store. */
export interface StoreUser {
  id: string;
  name: string;
  displayName: string;
  organization: string;
  username: string;
  role: string;
  organizations?: Array<{ id: string; name: string }>;
  projects?: Array<{ id: string; name: string }>;
}

/**
 * Maps the login response onto the shape the store holds.
 *
 * The two disagreed, and nothing noticed because the processService facade was
 * typed `any`:
 *
 *  - the store requires `displayName` and `organization`, which the login
 *    endpoint has never returned — both were `undefined` at runtime, which is
 *    why the sidebar and profile page render blanks;
 *  - the server sends `role` as `u.Roles`, an array, while the store declares a
 *    single string, so `{user?.role}` rendered "ADMIN" only by accident of
 *    array-to-string coercion.
 *
 * Deriving them here keeps the guesswork in one place instead of at every
 * consumer.
 */
export function toStoreUser(user: ApiUser): StoreUser {
  const organizations = user.organizations ?? [];
  return {
    id: user.id,
    name: user.name,
    username: user.username,
    displayName: user.name || user.username,
    organization: organizations[0]?.name ?? '',
    role: Array.isArray(user.role) ? user.role.join(', ') : (user.role ?? ''),
    organizations,
    projects: user.projects ?? [],
  };
}
