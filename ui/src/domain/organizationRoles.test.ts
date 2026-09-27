import { describe, expect, it } from 'bun:test';

import { hasRole } from './access';
import { viewerAccess, whereHeld } from './organizationRoles';

/*
 * An account holds a role in every organization it belongs to, or in the one
 * being worked in alone. The server acts with both in a request for this
 * organization; the page shows which is which, because only a platform
 * administrator can change the first kind.
 */
describe('where an account holds a role', () => {
  const kim = { roles: ['designer'], organization_roles: ['ADMIN'] };

  it('is here when it is held in this organization alone', () => {
    expect(whereHeld(kim, 'ADMIN')).toBe('here');
  });

  it('is everywhere when it is held in every organization, whatever case it was written in', () => {
    expect(whereHeld(kim, 'DESIGNER')).toBe('everywhere');
  });

  it('is everywhere when it is held both ways, since only a platform administrator can take it away', () => {
    expect(whereHeld({ roles: ['ADMIN'], organization_roles: ['ADMIN'] }, 'ADMIN')).toBe('everywhere');
  });

  it('is not held otherwise, including by an account the list says nothing about', () => {
    expect(whereHeld(kim, 'OPERATOR')).toBe('not held');
    expect(whereHeld({}, 'ADMIN')).toBe('not held');
  });
});

describe('what the person looking may do here', () => {
  const signedIn = { role: 'DESIGNER', roles: ['DESIGNER'] };

  it('is what their own account says for this organization, once it has been read', () => {
    const viewer = viewerAccess(signedIn, {
      user: { roles: ['DESIGNER'], organization_roles: ['ADMIN'] },
      mayChangeGlobalRoles: false,
    });

    expect(hasRole(viewer, 'ADMIN')).toBe(true);
    expect(hasRole(viewer, 'DESIGNER')).toBe(true);
    expect(viewer.mayChangeGlobalRoles).toBe(false);
  });

  it('includes no role held in another organization: the server answers for this one', () => {
    // An administrator of Globex, working in Acme.
    const viewer = viewerAccess({ roles: [] }, { user: { roles: [], organization_roles: [] }, mayChangeGlobalRoles: false });
    expect(hasRole(viewer, 'ADMIN')).toBe(false);
  });

  it('is the sign-in’s roles, with no say over roles held everywhere, until the account has been read', () => {
    const viewer = viewerAccess({ role: 'ADMIN, DESIGNER' });

    expect(hasRole(viewer, 'ADMIN')).toBe(true);
    expect(viewer.mayChangeGlobalRoles).toBe(false);
  });

  it('is nothing for nobody', () => {
    expect(viewerAccess(null)).toEqual({ roles: [], mayChangeGlobalRoles: false });
  });

  it('may change roles held everywhere only when the server says so', () => {
    const viewer = viewerAccess(signedIn, { user: { roles: ['ADMIN'] }, mayChangeGlobalRoles: true });
    expect(viewer.mayChangeGlobalRoles).toBe(true);
  });
});
