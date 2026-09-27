/**
 * What saving the account dialog on the Platform access page sends.
 *
 * An account has two kinds of role. The ones held in every organization it
 * belongs to are part of the account, go with the account update, and are a
 * platform administrator's to change. The ones held in the organization being
 * worked in are that organization's, and go on their own, to
 * PUT /users/{id}/organization-roles. The dialog edits both; each is sent only
 * when it changed, and the first only by somebody who may change it — so an
 * organization's administrator saving somebody's name never asks the server to
 * change what they hold everywhere.
 */
import type { UserUpdate } from '../services/domains/identityService';
import type { ApiOrganizationUser, CreateUserPayload } from '../services/types';

/** The dialog's fields. */
export interface AccountFormValues {
  username: string;
  password: string;
  fullName: string;
  displayName: string;
  email: string;
  /** Roles held in the organization being worked in. */
  rolesHere: string[];
  /** Roles held in every organization the account belongs to. */
  rolesEverywhere: string[];
}

/**
 * What editing an account sends: the account update when anything in the
 * account changed, and the roles held here when they changed.
 */
export interface AccountEdit {
  update?: UserUpdate;
  rolesHere?: string[];
}

const folded = (roles: readonly string[] | undefined) =>
  new Set((roles ?? []).map((role) => role.trim().toLowerCase()).filter((role) => role !== ''));

/** Whether two lists hold the same roles, in any order and case, as the server compares them. */
export function sameRoles(a: readonly string[] | undefined, b: readonly string[] | undefined): boolean {
  const left = folded(a);
  const right = folded(b);
  return left.size === right.size && [...left].every((role) => right.has(role));
}

/** The dialog's fields for an account, or empty ones for a new account. */
export function accountForm(account: ApiOrganizationUser | null): AccountFormValues {
  return {
    username: account?.username ?? '',
    password: '',
    fullName: account?.full_name ?? '',
    displayName: account?.display_name ?? '',
    email: account?.email ?? '',
    rolesHere: [...(account?.organization_roles ?? [])],
    rolesEverywhere: [...(account?.roles ?? [])],
  };
}

/**
 * The requests that save an edited account.
 *
 * The account update goes only when something in the account changed: its
 * names, its email, or — from somebody who may change them — the roles it holds
 * everywhere. It writes the names and email it is sent, so they go together;
 * the roles held everywhere are left out unless they changed, which the server
 * reads as "as they are". Sending it for nothing would also be refused for an
 * account another organization shares, which this organization's
 * administrators may still grant roles here.
 */
export function accountEdit(account: ApiOrganizationUser, form: AccountFormValues, mayChangeGlobalRoles: boolean): AccountEdit {
  const edit: AccountEdit = {};
  const update: UserUpdate = { full_name: form.fullName, display_name: form.displayName, email: form.email };
  const globalRolesChanged = mayChangeGlobalRoles && !sameRoles(account.roles, form.rolesEverywhere);
  if (globalRolesChanged) {
    update.roles = form.rolesEverywhere;
  }
  const profileChanged =
    form.fullName !== (account.full_name ?? '') ||
    form.displayName !== (account.display_name ?? '') ||
    form.email !== (account.email ?? '');
  if (profileChanged || globalRolesChanged) {
    edit.update = update;
  }
  if (!sameRoles(account.organization_roles, form.rolesHere)) {
    edit.rolesHere = form.rolesHere;
  }
  return edit;
}

/**
 * The new account a dialog creates, in the organization being worked in,
 * holding the roles chosen for here — and roles everywhere only when the
 * person creating it may give those.
 */
export function accountCreation(
  organization: { id: string; name: string },
  form: AccountFormValues,
  mayChangeGlobalRoles: boolean,
): CreateUserPayload {
  return {
    organization_id: organization.id,
    organization: organization.name,
    username: form.username,
    password: form.password,
    full_name: form.fullName,
    display_name: form.displayName,
    email: form.email,
    roles: mayChangeGlobalRoles ? form.rolesEverywhere : [],
    organization_roles: form.rolesHere,
  };
}
