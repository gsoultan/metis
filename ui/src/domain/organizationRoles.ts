/**
 * Where somebody holds a role: in every organization their account belongs to,
 * or in the organization being worked in alone.
 *
 * A role on the account is held in every organization it belongs to, and only
 * a platform administrator may change one. A role on the account's membership
 * of an organization is held there alone, and that organization's
 * administrators grant and revoke it. A request acts with both: the server
 * checks every role in the organization the request is for.
 *
 * This decides what to show and what to offer, never what is allowed — the
 * server checks again on every change.
 */
import type { ApiOrganizationUser } from '../services/types';
import { hasRole, type RoleHolder } from './access';
import { splitRoles } from './roles';

/** Where an account holds one role, as the organization's own list says. */
export type RoleHeld = 'everywhere' | 'here' | 'not held';

/** Where the account holds the role. Held everywhere wins: it is held here too, and only a platform administrator can take it away. */
export function whereHeld(account: Pick<ApiOrganizationUser, 'roles' | 'organization_roles'>, role: string): RoleHeld {
  if (hasRole({ roles: account.roles ?? [] }, role)) return 'everywhere';
  if (hasRole({ roles: account.organization_roles ?? [] }, role)) return 'here';
  return 'not held';
}

/** The signed-in account as the server describes it on its own profile. */
export interface OwnAccess {
  user?: Pick<ApiOrganizationUser, 'roles' | 'organization_roles'>;
  mayChangeGlobalRoles: boolean;
}

/** What the person looking may do in the organization being worked in. */
export interface ViewerAccess extends RoleHolder {
  roles: string[];
  /** Whether they may grant and take away roles held in every organization. */
  mayChangeGlobalRoles: boolean;
}

/**
 * The person looking: the roles they act with here — the ones held in every
 * organization and the ones held in this one — and whether they may change the
 * first kind.
 *
 * Until their own account has been read, the roles the sign-in listed, which
 * are the ones held in every organization, and no say over them: the sign-in
 * happens before there is an organization to ask about.
 */
export function viewerAccess(signedIn: RoleHolder | null | undefined, own?: OwnAccess): ViewerAccess {
  if (own?.user) {
    return {
      roles: [...(own.user.roles ?? []), ...(own.user.organization_roles ?? [])],
      mayChangeGlobalRoles: own.mayChangeGlobalRoles,
    };
  }
  const roles = signedIn?.roles ?? splitRoles(signedIn?.role ?? '');
  return { roles: [...roles], mayChangeGlobalRoles: false };
}
