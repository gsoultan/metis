import { describe, expect, it } from 'bun:test';

import { toStoreUser } from '../mappers/userMapper';
import { hasRole } from './access';

/**
 * Whether the signed-in person holds a role, for deciding what to show them.
 *
 * The pages asked `user.role === 'ADMIN'`. The store keeps every role joined
 * into that one string, so somebody holding ADMIN and anything else — or ADMIN
 * spelled the way older accounts spell it — was not shown the administrator's
 * controls the server would have let them use.
 */
describe('hasRole', () => {
  it('finds a role among several', () => {
    expect(hasRole({ roles: ['USER', 'ADMIN'], role: 'USER' }, 'ADMIN')).toBe(true);
  });

  it('recognises an administrator who holds another role too, once signed in', () => {
    // The login reply carries the roles as a list under `role`.
    const user = toStoreUser({ id: 'u-1', name: 'Dana', username: 'dana', role: ['USER', 'ADMIN'] });
    expect(hasRole(user, 'ADMIN')).toBe(true);
  });

  it('ignores case, as the server does', () => {
    // Accounts written by the older role picker hold "admin".
    expect(hasRole({ roles: ['admin'] }, 'ADMIN')).toBe(true);
  });

  it('reads the single-role field when there is no list', () => {
    expect(hasRole({ role: 'ADMIN' }, 'ADMIN')).toBe(true);
    // A session saved before the store kept the list holds the roles joined.
    expect(hasRole({ role: 'USER, ADMIN' }, 'ADMIN')).toBe(true);
  });

  it('does not let the single-role field add to the list', () => {
    expect(hasRole({ roles: ['USER'], role: 'ADMIN' }, 'ADMIN')).toBe(false);
  });

  it('does not match part of a role', () => {
    expect(hasRole({ roles: ['ADMINISTRATIVE_ASSISTANT'] }, 'ADMIN')).toBe(false);
  });

  it('denies when there is nobody, or nothing held', () => {
    expect(hasRole(null, 'ADMIN')).toBe(false);
    expect(hasRole(undefined, 'ADMIN')).toBe(false);
    expect(hasRole({ roles: [] }, 'ADMIN')).toBe(false);
    expect(hasRole({ role: '' }, 'ADMIN')).toBe(false);
    expect(hasRole({ roles: [''] }, '')).toBe(false);
  });
});
