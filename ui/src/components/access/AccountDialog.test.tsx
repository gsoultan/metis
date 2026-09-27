/**
 * The account dialog on Platform access holds both kinds of role: the ones held
 * in the organization being worked in, which its administrators grant, and the
 * ones held in every organization, which only a platform administrator may
 * change. Everybody else is shown the second kind and cannot change it.
 */
import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { MantineProvider, Modal, createTheme } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderToStaticMarkup } from 'react-dom/server';

import type { OwnProfile } from '../../services/domains/identityService';
import type { ApiOrganizationUser } from '../../services/types';
import { standInForAppStore, userWithRoles } from '../../test/appStoreStandIn';
import { labelledControl, visibleText } from '../../testing/markup';
import { AccountDialog } from './AccountDialog';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const ORGANIZATION = 'org-1';
const acme = { id: ORGANIZATION, name: 'Acme' };

beforeEach(() => {
  store.set({ user: userWithRoles(['ADMIN']), currentOrganizationId: ORGANIZATION, token: 'a-session' });
});

// A modal renders into a portal, which a server render leaves empty.
const theme = createTheme({ components: { Modal: Modal.extend({ defaultProps: { withinPortal: false } }) } });

/** Kim designs in Acme and operates in every organization she belongs to. */
const kim: ApiOrganizationUser = {
  id: 'u-kim',
  username: 'kim',
  full_name: 'Kim Park',
  email: 'kim@example.com',
  organization: { id: ORGANIZATION, name: 'Acme' },
  roles: ['OPERATOR'],
  organization_roles: ['DESIGNER'],
};

function render(account: ApiOrganizationUser | null, own: OwnProfile): string {
  const client = new QueryClient();
  client.setQueryData(['own-profile', 'user-1', ORGANIZATION], own);
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MantineProvider theme={theme}>
        <AccountDialog opened onClose={() => {}} account={account} organization={acme} />
      </MantineProvider>
    </QueryClientProvider>,
  );
}

const administratorHere: OwnProfile = { user: { roles: [], organization_roles: ['ADMIN'] }, mayChangeGlobalRoles: false };
const platformAdministrator: OwnProfile = { user: { roles: ['ADMIN'] }, mayChangeGlobalRoles: true };

describe('the roles an account holds', () => {
  it('are shown both ways, each where it is held', () => {
    const text = visibleText(render(kim, administratorHere));

    expect(text).toContain('Roles in this organization');
    expect(text).toContain('Designer — Authors and deploys process and decision models');
    expect(text).toContain('Roles in every organization');
    expect(text).toContain('Operator — Resolves incidents');
  });

  it('in this organization can be changed by its administrator', () => {
    const control = labelledControl(render(kim, administratorHere), 'Roles in this organization');
    expect(control).toBeDefined();
    expect(control).not.toHaveProperty('disabled');
  });

  it('in every organization cannot be changed by anybody the platform gate refuses, who is told who can', () => {
    const html = render(kim, administratorHere);

    expect(labelledControl(html, 'Roles in every organization')).toHaveProperty('disabled');
    expect(visibleText(html)).toContain('Only a platform administrator can change these.');
  });

  it('in every organization can be changed by a platform administrator', () => {
    const html = render(kim, platformAdministrator);
    const control = labelledControl(html, 'Roles in every organization');

    expect(control).toBeDefined();
    expect(control).not.toHaveProperty('disabled');
    expect(visibleText(html)).not.toContain('Only a platform administrator can change these.');
  });

  it('start empty for a new account, which joins the organization being worked in', () => {
    const html = render(null, administratorHere);

    expect(visibleText(html)).toContain('New people join the organization you are working in');
    expect(labelledControl(html, 'Roles in every organization')).toHaveProperty('disabled');
  });
});
