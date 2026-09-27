import { afterEach, describe, expect, it } from 'bun:test';

import en from '../i18n/catalogues/en';
import { format } from '../i18n/translate';
import { identityService } from '../services/domains/identityService';
import { stubFetch } from '../services/shared/stubbedFetch';
import type { ApiOrganizationUser } from '../services/types';
import {
  revokesOwnAdministration,
  roleChangeNotice,
  roleUpdate,
  rolesWith,
  sendRoleChange,
  withAccountRoles,
  type AccountList,
  type RoleUpdate,
} from './roleChange';

/** Dana designs in this organization, and holds USER — no column in the matrix — in every one. */
const dana: ApiOrganizationUser = {
  id: 'u-dana',
  username: 'dana',
  full_name: 'Dana Scully',
  display_name: 'Scully',
  email: 'dana@example.com',
  organization: { id: 'org-1', name: 'Acme' },
  roles: ['USER'],
  organization_roles: ['DESIGNER'],
};

const t = (key: string, values?: Record<string, string | number>) => format(en, key, values);

/** What the matrix sends a change through: the roles held in the organization being worked in. */
const save = ({ id, roles }: RoleUpdate) => identityService.setOrganizationRoles(id, roles);

describe('one role granted or revoked', () => {
  it('adds the role as the server spells it, and keeps every other role', () => {
    expect(rolesWith(['DESIGNER', 'USER'], 'OPERATOR', true)).toEqual(['DESIGNER', 'USER', 'OPERATOR']);
  });

  it('takes the role away in whatever case the account holds it, and nothing else', () => {
    // "USER" has no column in the matrix, and nobody clicked it.
    expect(rolesWith(['designer', 'USER', 'Designer'], 'DESIGNER', false)).toEqual(['USER']);
  });

  it('holds a role once, however it was written before', () => {
    expect(rolesWith(['admin'], 'ADMIN', true)).toEqual(['ADMIN']);
  });

  it('reads an account with no roles as holding none', () => {
    expect(rolesWith(undefined, 'ADMIN', true)).toEqual(['ADMIN']);
    expect(rolesWith(undefined, 'ADMIN', false)).toEqual([]);
  });
});

/*
 * A tick changed the account's own roles, through the account update, and
 * those every organization the account belongs to shares: granting a role in
 * one organization's matrix granted it in all of them. A tick now changes the
 * roles held in the organization being worked in, and nothing else.
 */
describe('the update a checkbox sends', () => {
  it('is the roles held in this organization with one changed, and not the ones held everywhere', () => {
    expect(roleUpdate(dana, 'OPERATOR', true)).toEqual({ id: 'u-dana', roles: ['DESIGNER', 'OPERATOR'] });
    expect(roleUpdate(dana, 'DESIGNER', false)).toEqual({ id: 'u-dana', roles: [] });
  });

  it('starts from none for somebody who holds nothing here', () => {
    expect(roleUpdate({ id: 'u-new', username: 'new' }, 'ADMIN', true)).toEqual({ id: 'u-new', roles: ['ADMIN'] });
  });

  it('reaches the server as the roles for this organization, and names nothing else about the account', async () => {
    const stub = stubFetch({});
    try {
      await sendRoleChange(roleUpdate(dana, 'OPERATOR', true), save);

      expect(stub.sent[0].method).toBe('PUT');
      expect(stub.sent[0].url.endsWith('/users/u-dana/organization-roles')).toBe(true);
      // No names, no email, no roles held everywhere: the account is not what changes.
      expect(stub.sent[0].body).toEqual({ roles: ['DESIGNER', 'OPERATOR'] });
    } finally {
      stub.restore();
    }
  });
});

describe('after the server accepts a change', () => {
  const ana: ApiOrganizationUser = { id: 'u-ana', username: 'ana', full_name: 'Ana', organization_roles: ['ADMIN'] };
  const list: AccountList = { users: [ana, dana] };

  it('holds the account’s roles here as the server now does, and leaves everybody else as they were', () => {
    const after = withAccountRoles(list, 'u-dana', ['DESIGNER', 'OPERATOR']);

    expect(after?.users).toEqual([ana, { ...dana, organization_roles: ['DESIGNER', 'OPERATOR'] }]);
    // The roles held everywhere are the account's, and a tick does not touch them.
    expect(after?.users[1].roles).toEqual(['USER']);
    // A new list, not the old one changed underneath whoever else holds it.
    expect(list.users[1].organization_roles).toEqual(['DESIGNER']);
  });

  it('starts the next change to the same person from the one before', () => {
    const first = roleUpdate(dana, 'OPERATOR', true);
    const after = withAccountRoles(list, first.id, first.roles);
    const second = roleUpdate(after!.users[1], 'ADMIN', true);

    // Worked out from the list as it was, the second change would have sent
    // DESIGNER and ADMIN — and taken Operator away again.
    expect(second.roles).toEqual(['DESIGNER', 'OPERATOR', 'ADMIN']);
  });

  it('leaves a list nobody has read yet as it is', () => {
    expect(withAccountRoles(undefined, 'u-dana', ['ADMIN'])).toBeUndefined();
  });
});

describe('taking the administrator role from yourself', () => {
  it('is the one change asked about first', () => {
    expect(revokesOwnAdministration({ ...dana, id: 'me' }, 'ADMIN', false, 'me')).toBe(true);
    expect(revokesOwnAdministration({ ...dana, id: 'me' }, 'admin', false, 'me')).toBe(true);
  });

  it('is not somebody else’s, a grant, or another role', () => {
    expect(revokesOwnAdministration(dana, 'ADMIN', false, 'me')).toBe(false);
    expect(revokesOwnAdministration({ ...dana, id: 'me' }, 'ADMIN', true, 'me')).toBe(false);
    expect(revokesOwnAdministration({ ...dana, id: 'me' }, 'DESIGNER', false, 'me')).toBe(false);
  });
});

describe('what the person is told', () => {
  let restore = () => {};
  afterEach(() => restore());

  // The server's words, as tests/user/role_matrix_test.go holds them. The
  // "forbidden: " in front is the class a transport answers 403 by, not
  // something to tell the person.
  const lastAdministrator = 'dana is the last administrator of Acme; make somebody else an administrator first';

  it('is the server’s refusal in its own words, not swallowed', async () => {
    ({ restore } = stubFetch({ error: `forbidden: ${lastAdministrator}` }, 403));

    const outcome = await sendRoleChange(roleUpdate(dana, 'ADMIN', false), save);

    expect(outcome).toEqual({ changed: false, reason: lastAdministrator });
    expect(roleChangeNotice(outcome, { name: 'Dana Scully', role: 'Administrator', granted: false }, t)).toEqual({
      title: 'Could not change Dana Scully’s roles',
      message: lastAdministrator,
      color: 'red',
    });
  });

  // A tick for an account another organization shares is this organization's
  // business now, and is not refused for it. Somebody who administers another
  // organization, and not this one, still is.
  it('is the server’s refusal to somebody who administers another organization and not this one, too', async () => {
    const notHere =
      'this needs the ADMIN role, which your account does not hold in this organization; an administrator here can grant it';
    ({ restore } = stubFetch({ error: `forbidden: ${notHere}` }, 403));

    const outcome = await sendRoleChange(roleUpdate(dana, 'OPERATOR', true), save);

    expect(outcome).toEqual({ changed: false, reason: notHere });
  });

  it('leaves the account as the list holds it when the change is refused', async () => {
    const before = structuredClone(dana);
    await sendRoleChange(roleUpdate(dana, 'ADMIN', true), async () => {
      throw new Error('refused');
    });
    expect(dana).toEqual(before);
  });

  it('says what changed when it was made', async () => {
    const saved: RoleUpdate[] = [];
    const outcome = await sendRoleChange(roleUpdate(dana, 'OPERATOR', true), async (update) => {
      saved.push(update);
    });

    expect(saved).toHaveLength(1);
    expect(roleChangeNotice(outcome, { name: 'Dana Scully', role: 'Operator', granted: true }, t)).toEqual({
      title: 'Roles changed',
      message: 'Dana Scully now holds Operator.',
      color: 'green',
    });
    expect(roleChangeNotice(outcome, { name: 'Dana Scully', role: 'Operator', granted: false }, t).message).toBe(
      'Dana Scully no longer holds Operator.',
    );
  });
});
