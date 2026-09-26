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

const dana: ApiOrganizationUser = {
  id: 'u-dana',
  username: 'dana',
  full_name: 'Dana Scully',
  display_name: 'Scully',
  email: 'dana@example.com',
  organization: { id: 'org-1', name: 'Acme' },
  roles: ['designer', 'USER'],
};

const t = (key: string, values?: Record<string, string | number>) => format(en, key, values);

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
 * The user update writes the name, display name and email it is sent, an
 * empty one included. A change that sent only the roles would have blanked
 * all three.
 */
describe('the update a checkbox sends', () => {
  it('carries the account’s names and email as they are, beside the new roles', () => {
    expect(roleUpdate(dana, 'OPERATOR', true)).toEqual({
      id: 'u-dana',
      full_name: 'Dana Scully',
      display_name: 'Scully',
      email: 'dana@example.com',
      roles: ['designer', 'USER', 'OPERATOR'],
    });
  });

  it('reaches the server with nothing blanked and nothing else claimed', async () => {
    const stub = stubFetch({});
    try {
      const update = roleUpdate(dana, 'DESIGNER', false);
      await sendRoleChange(update, ({ id, ...user }) => identityService.updateUser(id, user));

      expect(stub.sent[0].method).toBe('PUT');
      expect(stub.sent[0].url.endsWith('/users/u-dana')).toBe(true);
      // No username, no organization: the server keeps those as they are.
      expect((stub.sent[0].body as { user: unknown }).user).toEqual({
        id: 'u-dana',
        full_name: 'Dana Scully',
        display_name: 'Scully',
        email: 'dana@example.com',
        roles: ['USER'],
      });
    } finally {
      stub.restore();
    }
  });
});

describe('after the server accepts a change', () => {
  const ana: ApiOrganizationUser = { id: 'u-ana', username: 'ana', full_name: 'Ana', roles: ['ADMIN'] };
  const list: AccountList = { users: [ana, dana] };

  it('holds the account’s roles as the server now does, and leaves everybody else as they were', () => {
    const after = withAccountRoles(list, 'u-dana', ['USER', 'OPERATOR']);

    expect(after?.users).toEqual([ana, { ...dana, roles: ['USER', 'OPERATOR'] }]);
    // A new list, not the old one changed underneath whoever else holds it.
    expect(list.users[1].roles).toEqual(['designer', 'USER']);
  });

  it('starts the next change to the same person from the one before', () => {
    const first = roleUpdate(dana, 'OPERATOR', true);
    const after = withAccountRoles(list, first.id, first.roles);
    const second = roleUpdate(after!.users[1], 'ADMIN', true);

    // Worked out from the list as it was, the second change would have sent
    // designer, USER and ADMIN — and taken Operator away again.
    expect(second.roles).toEqual(['designer', 'USER', 'OPERATOR', 'ADMIN']);
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
    const update = roleUpdate(dana, 'ADMIN', false);

    const outcome = await sendRoleChange(update, ({ id, ...user }) => identityService.updateUser(id, user));

    expect(outcome).toEqual({ changed: false, reason: lastAdministrator });
    expect(roleChangeNotice(outcome, { name: 'Dana Scully', role: 'Administrator', granted: false }, t)).toEqual({
      title: 'Could not change Dana Scully’s roles',
      message: lastAdministrator,
      color: 'red',
    });
  });

  it('is the server’s refusal for an account another organization shares, too', async () => {
    const shared =
      'dana also belongs to another organization, which you are not a member of; an administrator there has to make this change';
    ({ restore } = stubFetch({ error: `forbidden: ${shared}` }, 403));

    const outcome = await sendRoleChange(roleUpdate(dana, 'OPERATOR', true), ({ id, ...user }) =>
      identityService.updateUser(id, user),
    );

    expect(outcome).toEqual({ changed: false, reason: shared });
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
