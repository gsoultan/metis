import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { createElement } from 'react';

import id from '../../i18n/catalogues/id';
import type { ApiRoleAccess } from '../../services/domains/roleService';
import type { ApiOrganizationUser } from '../../services/types';
import { standInForAppStore, userWithRoles } from '../../test/appStoreStandIn';
import { inLanguage, renderStatic, visibleText } from '../../test/renderStatic';
import { namedControl } from '../../testing/markup';
import { RoleMatrix } from './RoleMatrix';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const ORGANIZATION = 'org-1';

beforeEach(() => {
  store.set({ user: userWithRoles(['ADMIN']), currentOrganizationId: ORGANIZATION, token: 'a-session' });
});

const ana: ApiOrganizationUser = {
  id: 'u-ana', username: 'ana', full_name: 'Ana Admin', display_name: 'Ana', email: 'ana@example.com', roles: ['ADMIN'],
};
// Written by the older picker, in lowercase. The server matches roles
// case-insensitively, and so must the matrix.
const dana: ApiOrganizationUser = {
  id: 'u-dana', username: 'dana', full_name: 'Dana Scully', display_name: 'Dana', email: 'dana@example.com', roles: ['designer'],
};
const oli: ApiOrganizationUser = {
  id: 'u-oli', username: 'oli', full_name: '', display_name: '', email: '', roles: ['OPERATOR', 'QUERY_AUTHOR', 'USER'],
};

const legend: ApiRoleAccess[] = [
  { role: 'ADMIN', actions: [{ method: 'UpdateUser', area: 'accounts', label: 'Update user' }] },
  { role: 'DESIGNER', actions: [{ method: 'CreateDefinition', area: 'processes', label: 'Create definition' }] },
  { role: 'OPERATOR', actions: [{ method: 'ResolveIncident', area: 'instances', label: 'Resolve incident' }] },
  { role: 'QUERY_AUTHOR', actions: [] },
];

/** The answers the Accounts view's list and the legend give, under the keys the matrix asks with. */
function answered(users: ApiOrganizationUser[]): QueryClient {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  client.setQueryData(['users', ORGANIZATION], { users });
  client.setQueryData(['roles'], legend);
  return client;
}

const render = (users: ApiOrganizationUser[]) => renderStatic(createElement(RoleMatrix), answered(users));

/** Somebody signed in who administers nothing: a designer. */
const asDesigner = () => store.set({ user: userWithRoles(['DESIGNER']), currentOrganizationId: ORGANIZATION, token: 'a-session' });

/*
 * Roles were granted one account at a time, from a multi-select inside each
 * account's edit dialog, and nothing showed at a glance who held what.
 */
describe('who holds which role', () => {
  it('marks, for every account, each role it holds and each it does not', async () => {
    asDesigner();
    const text = visibleText(await render([ana, dana, oli]));

    expect(text).toContain('Ana Admin holds Administrator');
    expect(text).toContain('Ana Admin does not hold Designer');
    expect(text).toContain('Dana Scully holds Designer');
    expect(text).toContain('Dana Scully does not hold Administrator');
    // An account with no full name is named by its username, as on the Accounts view.
    expect(text).toContain('oli holds Operator');
    expect(text).toContain('oli holds Query author');
  });

  it('shows every account in the organization, not a first page', async () => {
    asDesigner();
    const many = Array.from({ length: 60 }, (_, index): ApiOrganizationUser => ({
      id: `u-${index}`, username: `person${index}`, full_name: `Person ${index}`, roles: index % 2 === 0 ? ['DESIGNER'] : [],
    }));
    const text = visibleText(await render(many));

    expect(text.match(/Person \d+ (?:holds|does not hold) Designer/g)).toHaveLength(60);
    expect(text).toContain('Showing all 60 accounts');
  });

  it('puts what each role is required for one button away from its heading', async () => {
    const html = await render([ana]);
    for (const role of ['Administrator', 'Designer', 'Operator', 'Query author']) {
      expect(namedControl(html, `What ${role} allows`)).toBeDefined();
    }
  });

  it('says so when the organization has nobody in it yet', async () => {
    expect(visibleText(await render([]))).toContain('No accounts in this organization yet.');
  });

  it('reads in the interface’s language', async () => {
    asDesigner();
    const html = await renderStatic(inLanguage(createElement(RoleMatrix), 'id', id), answered([ana, dana]));
    const text = visibleText(html);
    expect(text).toContain('Dana Scully memegang Designer');
    expect(text).toContain('Menampilkan semua 2 akun');
    expect(namedControl(html, 'Yang diizinkan Designer')).toBeDefined();
  });
});

/*
 * The server refuses a role change from anybody but an administrator, and the
 * matrix does not offer one: a box that can only ever be refused is not a
 * control.
 */
describe('granting and revoking', () => {
  it('gives an administrator a box per person and role, ticked where the server says it is held', async () => {
    const html = await render([ana, dana, oli]);

    expect(namedControl(html, 'Administrator for Ana Admin')).toHaveProperty('checked');
    // Held as "designer", which is Designer.
    expect(namedControl(html, 'Designer for Dana Scully')).toHaveProperty('checked');
    expect(namedControl(html, 'Administrator for Dana Scully')).not.toHaveProperty('checked');
    expect(namedControl(html, 'Query author for oli')).toHaveProperty('checked');
    expect(namedControl(html, 'Designer for oli')).not.toHaveProperty('checked');
  });

  it('tells an administrator that each change is saved as it is made', async () => {
    expect(visibleText(await render([ana]))).toContain(
      'Tick a box to grant a role and clear it to take the role away. Each change is saved as you make it.',
    );
  });

  it('shows anybody else no box to tick, and says who can change roles', async () => {
    asDesigner();
    const html = await render([ana, dana]);

    expect(html).not.toContain('type="checkbox"');
    expect(namedControl(html, 'Designer for Dana Scully')).toBeUndefined();
    expect(visibleText(html)).toContain('Only an administrator can change who holds a role.');
  });
});
