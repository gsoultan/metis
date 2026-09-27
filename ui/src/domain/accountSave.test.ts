import { describe, expect, it } from 'bun:test';

import type { ApiOrganizationUser } from '../services/types';
import { accountCreation, accountEdit, accountForm, sameRoles } from './accountSave';

/** Kim designs in this organization and operates in every one she belongs to. */
const kim: ApiOrganizationUser = {
  id: 'u-kim',
  username: 'kim',
  full_name: 'Kim Park',
  display_name: 'Kim',
  email: 'kim@example.com',
  roles: ['OPERATOR'],
  organization_roles: ['DESIGNER'],
};

describe('two lists of roles', () => {
  it('are the same whatever their order and case', () => {
    expect(sameRoles(['ADMIN', 'designer'], ['DESIGNER', 'admin'])).toBe(true);
  });

  it('differ by a role either one holds alone', () => {
    expect(sameRoles(['ADMIN'], ['ADMIN', 'DESIGNER'])).toBe(false);
    expect(sameRoles(undefined, ['ADMIN'])).toBe(false);
    expect(sameRoles(undefined, [])).toBe(true);
  });
});

describe('the dialog for an account', () => {
  it('starts from the account: its names, its roles here and its roles everywhere', () => {
    expect(accountForm(kim)).toEqual({
      username: 'kim',
      password: '',
      fullName: 'Kim Park',
      displayName: 'Kim',
      email: 'kim@example.com',
      rolesHere: ['DESIGNER'],
      rolesEverywhere: ['OPERATOR'],
    });
  });

  it('starts empty for a new account', () => {
    expect(accountForm(null).rolesHere).toEqual([]);
    expect(accountForm(null).rolesEverywhere).toEqual([]);
  });
});

/*
 * The dialog saved every field through the account update, roles included —
 * and the account's roles are the ones every organization it belongs to
 * shares. Now each kind of role goes where it is held, and only when it
 * changed.
 */
describe('saving an edited account', () => {
  it('sends a role granted here as this organization’s roles, and leaves the account alone', () => {
    const edit = accountEdit(kim, { ...accountForm(kim), rolesHere: ['DESIGNER', 'ADMIN'] }, false);

    expect(edit.rolesHere).toEqual(['DESIGNER', 'ADMIN']);
    // Nothing about the account changed, so the account update is not sent at
    // all — it would be refused for an account another organization shares.
    expect(edit.update).toBeUndefined();
  });

  it('sends a changed name through the account update, without the roles held everywhere', () => {
    const edit = accountEdit(kim, { ...accountForm(kim), fullName: 'Kim Lee' }, false);

    expect(edit.update).toEqual({ full_name: 'Kim Lee', display_name: 'Kim', email: 'kim@example.com' });
    expect(edit.update).not.toHaveProperty('roles');
    expect(edit.rolesHere).toBeUndefined();
  });

  it('sends the roles held everywhere only from somebody who may change them, and only when they changed', () => {
    const changed = { ...accountForm(kim), rolesEverywhere: ['OPERATOR', 'ADMIN'] };

    expect(accountEdit(kim, changed, true).update).toEqual({
      full_name: 'Kim Park',
      display_name: 'Kim',
      email: 'kim@example.com',
      roles: ['OPERATOR', 'ADMIN'],
    });
    expect(accountEdit(kim, changed, false).update).toBeUndefined();
    expect(accountEdit(kim, accountForm(kim), true)).toEqual({});
  });
});

describe('creating an account', () => {
  const acme = { id: 'org-1', name: 'Acme' };
  const form = {
    ...accountForm(null),
    username: 'lee',
    password: 'a-password-long-enough',
    fullName: 'Lee Chan',
    rolesHere: ['DESIGNER'],
    rolesEverywhere: ['ADMIN'],
  };

  it('puts it in this organization holding the roles chosen for here', () => {
    const created = accountCreation(acme, form, false);

    expect(created.organization_id).toBe('org-1');
    expect(created.organization_roles).toEqual(['DESIGNER']);
    // Only a platform administrator gives roles held everywhere.
    expect(created.roles).toEqual([]);
  });

  it('gives it roles held everywhere when a platform administrator creates it', () => {
    expect(accountCreation(acme, form, true).roles).toEqual(['ADMIN']);
  });
});
