import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { createElement } from 'react';

import id from '../../i18n/catalogues/id';
import type { OwnProfile } from '../../services/domains/identityService';
import type { ApiRoleAccess } from '../../services/domains/roleService';
import type { ApiOrganizationUser } from '../../services/types';
import { standInForAppStore, userWithRoles } from '../../test/appStoreStandIn';
import { inLanguage, renderStatic, visibleText } from '../../test/renderStatic';
import { namedControl } from '../../testing/markup';
import { RoleMatrix } from './RoleMatrix';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const ORGANIZATION = 'org-1';

/** Somebody signed in holding these roles in every organization, working in ORGANIZATION. */
const signIn = (roles: string[]) =>
  store.set({ user: userWithRoles(roles), currentOrganizationId: ORGANIZATION, token: 'a-session' });

beforeEach(() => signIn(['ADMIN']));

// Roles held in this organization, which its administrators grant and revoke.
const ana: ApiOrganizationUser = {
  id: 'u-ana', username: 'ana', full_name: 'Ana Admin', display_name: 'Ana', email: 'ana@example.com', organization_roles: ['ADMIN'],
};
const dana: ApiOrganizationUser = {
  id: 'u-dana', username: 'dana', full_name: 'Dana Scully', display_name: 'Dana', email: 'dana@example.com', organization_roles: ['DESIGNER'],
};
// USER has no column: it is held everywhere and gates nothing.
const oli: ApiOrganizationUser = {
  id: 'u-oli', username: 'oli', full_name: '', display_name: '', email: '', roles: ['USER'], organization_roles: ['OPERATOR', 'QUERY_AUTHOR'],
};
// A designer in every organization, written by the older picker in lowercase.
// The server matches roles case-insensitively, and so must the matrix.
const gus: ApiOrganizationUser = { id: 'u-gus', username: 'gus', full_name: 'Gus Global', roles: ['designer'] };

const legend: ApiRoleAccess[] = [
  { role: 'ADMIN', actions: [{ method: 'UpdateUser', area: 'accounts', label: 'Update user' }] },
  { role: 'DESIGNER', actions: [{ method: 'CreateDefinition', area: 'processes', label: 'Create definition' }] },
  { role: 'OPERATOR', actions: [{ method: 'ResolveIncident', area: 'instances', label: 'Resolve incident' }] },
  { role: 'QUERY_AUTHOR', actions: [] },
];

/**
 * The answers the Accounts view's list, the legend and — when given — the
 * signed-in account's own profile give, under the keys the matrix asks with.
 */
function answered(users: ApiOrganizationUser[], own?: OwnProfile): QueryClient {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  client.setQueryData(['users', ORGANIZATION], { users });
  client.setQueryData(['roles'], legend);
  if (own) client.setQueryData(['own-profile', 'user-1', ORGANIZATION], own);
  return client;
}

const render = (users: ApiOrganizationUser[], own?: OwnProfile) => renderStatic(createElement(RoleMatrix), answered(users, own));

/** Somebody signed in who administers nothing: a designer. */
const asDesigner = () => signIn(['DESIGNER']);

/*
 * Roles were granted one account at a time, from a multi-select inside each
 * account's edit dialog, and nothing showed at a glance who held what.
 */
describe('who holds which role', () => {
  it('marks, for every account, each role it holds here, each it holds everywhere, and each it does not hold', async () => {
    asDesigner();
    const text = visibleText(await render([ana, dana, oli, gus]));

    expect(text).toContain('Ana Admin holds Administrator');
    expect(text).toContain('Ana Admin does not hold Designer');
    expect(text).toContain('Dana Scully holds Designer');
    expect(text).toContain('Dana Scully does not hold Administrator');
    // An account with no full name is named by its username, as on the Accounts view.
    expect(text).toContain('oli holds Operator');
    expect(text).toContain('oli holds Query author');
    expect(text).toContain('Gus Global holds Designer in every organization');
  });

  it('shows every account in the organization, not a first page', async () => {
    asDesigner();
    const many = Array.from({ length: 60 }, (_, index): ApiOrganizationUser => ({
      id: `u-${index}`, username: `person${index}`, full_name: `Person ${index}`, organization_roles: index % 2 === 0 ? ['DESIGNER'] : [],
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
    const html = await renderStatic(inLanguage(createElement(RoleMatrix), 'id', id), answered([ana, dana, gus]));
    const text = visibleText(html);
    expect(text).toContain('Dana Scully memegang Designer');
    expect(text).toContain('Gus Global memegang Designer di setiap organisasi');
    expect(text).toContain('Menampilkan semua 3 akun');
    expect(namedControl(html, 'Yang diizinkan Designer')).toBeDefined();
  });
});

/*
 * The server refuses a role change in an organization from anybody but an
 * administrator there, and the matrix does not offer one: a box that can only
 * ever be refused is not a control. A role held in every organization is not
 * this organization's to change, so it has no box either.
 */
describe('granting and revoking', () => {
  it('gives an administrator a box per person and role, ticked where the account holds the role here', async () => {
    const html = await render([ana, dana, oli]);

    expect(namedControl(html, 'Administrator for Ana Admin')).toHaveProperty('checked');
    expect(namedControl(html, 'Designer for Dana Scully')).toHaveProperty('checked');
    expect(namedControl(html, 'Administrator for Dana Scully')).not.toHaveProperty('checked');
    expect(namedControl(html, 'Query author for oli')).toHaveProperty('checked');
    expect(namedControl(html, 'Designer for oli')).not.toHaveProperty('checked');
  });

  it('shows a role held in every organization as that, with no box, and says who changes it', async () => {
    const html = await render([dana, gus]);

    expect(namedControl(html, 'Designer for Gus Global')).toBeUndefined();
    // Gus's other roles are this organization's to grant.
    expect(namedControl(html, 'Operator for Gus Global')).not.toHaveProperty('checked');
    expect(visibleText(html)).toContain('Gus Global holds Designer in every organization');
    expect(visibleText(html)).toContain(
      'A role marked “Every organization” is held in every organization the account belongs to: only a platform administrator can change it',
    );
  });

  it('tells an administrator that each change is saved as it is made, in this organization', async () => {
    expect(visibleText(await render([ana]))).toContain(
      'Tick a box to grant a role in this organization and clear it to take the role away. Each change is saved as you make it.',
    );
  });

  it('gives the boxes to an administrator of this organization alone', async () => {
    signIn([]);
    const html = await render([ana, dana], { user: { roles: [], organization_roles: ['ADMIN'] }, mayChangeGlobalRoles: false });

    expect(namedControl(html, 'Designer for Dana Scully')).toHaveProperty('checked');
  });

  it('gives none to an administrator of another organization, working in this one', async () => {
    signIn([]);
    const html = await render([ana, dana], { user: { roles: [], organization_roles: [] }, mayChangeGlobalRoles: false });

    expect(html).not.toContain('type="checkbox"');
  });

  it('shows anybody else no box to tick, and says who can change roles', async () => {
    asDesigner();
    const html = await render([ana, dana]);

    expect(html).not.toContain('type="checkbox"');
    expect(namedControl(html, 'Designer for Dana Scully')).toBeUndefined();
    expect(visibleText(html)).toContain('Only an administrator of this organization can change who holds a role here.');
  });
});
