/**
 * Every close button a dialog draws says what it does.
 *
 * Mantine draws the close button of a modal, a drawer, a notification and an
 * alert as an icon and gives it no name, so a screen reader announced "button"
 * and axe reported button-name, rated critical, on every dialog in the app.
 * Three dialogs on the webhooks card had been named one at a time; the rest had
 * nothing. The name is a default of the theme now, in the interface's language.
 */
import { describe, expect, it } from 'bun:test';
import { Alert, Drawer, MantineProvider, Modal, Notification } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { renderToStaticMarkup } from 'react-dom/server';

import id from '../i18n/catalogues/id';
import { Route as AppRoot } from '../routes/__root';
import { inLanguage } from '../test/renderStatic';
import { LocalisedTheme } from './LocalisedTheme';
import { theme } from './index';

const noop = () => {};

/** Each kind of dialog, open, drawn in place rather than in a portal a server render leaves empty. */
function EveryDialog() {
  return (
    <>
      <Modal opened onClose={noop} title="A modal" withinPortal={false}>
        Body
      </Modal>
      <Drawer opened onClose={noop} title="A drawer" withinPortal={false}>
        Body
      </Drawer>
      <Notification title="A notification" onClose={noop}>
        Body
      </Notification>
      <Alert title="An alert" withCloseButton onClose={noop}>
        Body
      </Alert>
    </>
  );
}

function DialogThatNamesItsClose() {
  return (
    <Modal opened onClose={noop} title="A modal" withinPortal={false} closeButtonProps={{ 'aria-label': 'Stop editing' }}>
      Body
    </Modal>
  );
}

// The app's own root component, so the page is drawn inside every provider the
// app has. A root of its own rather than the app's route object, which a test
// should not rewire.
const root = createRootRoute({ component: AppRoot.options.component });
const routeTree = root.addChildren([
  createRoute({ getParentRoute: () => root, path: '/', component: EveryDialog }),
  createRoute({ getParentRoute: () => root, path: '/named', component: DialogThatNamesItsClose }),
]);

async function throughTheApp(path: string): Promise<string> {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  await router.load();
  return renderToStaticMarkup(
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

/** Each close button's accessible name, keyed by the part of the dialog it closes. */
function closeButtonNames(html: string): Record<string, string | undefined> {
  const names: Record<string, string | undefined> = {};
  for (const [tag] of html.matchAll(/<button[^>]*mantine-CloseButton-root[^>]*>/g)) {
    const part = /mantine-(Modal-close|Drawer-close|Notification-closeButton|Alert-closeButton)\b/.exec(tag)?.[1];
    if (part) names[part] = /\saria-label="([^"]*)"/.exec(tag)?.[1];
  }
  return names;
}

describe('the close button of every dialog', () => {
  it('is named, app-wide, without the dialog saying so', async () => {
    expect(closeButtonNames(await throughTheApp('/'))).toEqual({
      'Modal-close': 'Close',
      'Drawer-close': 'Close',
      'Notification-closeButton': 'Close',
      'Alert-closeButton': 'Close',
    });
  });

  it('is named in the interface’s language', () => {
    const html = renderToStaticMarkup(
      <MantineProvider theme={theme}>
        {inLanguage(
          <LocalisedTheme>
            <EveryDialog />
          </LocalisedTheme>,
          'id',
          id,
        )}
      </MantineProvider>,
    );
    expect(Object.values(closeButtonNames(html))).toEqual(['Tutup', 'Tutup', 'Tutup', 'Tutup']);
  });

  it('keeps a name a dialog gives its own close button', async () => {
    expect(closeButtonNames(await throughTheApp('/named'))['Modal-close']).toBe('Stop editing');
  });
});
