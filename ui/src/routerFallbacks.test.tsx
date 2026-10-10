import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { prerender } from 'react-dom/static';

import { routerFallbacks } from './routerFallbacks';
import { visibleText } from './test/renderStatic';

/** Renders `path` through a router with the app's fallbacks. */
async function renderAt(path: string): Promise<string> {
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({ getParentRoute: () => root, path: '/', component: () => <p>home</p> });
  const broken = createRoute({
    getParentRoute: () => root,
    path: '/broken',
    loader: () => {
      throw new Error('the instances could not be read');
    },
    component: () => <p>never</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([home, broken]),
    history: createMemoryHistory({ initialEntries: [path] }),
    ...routerFallbacks,
  });
  await router.load();
  // prerender, not renderToStaticMarkup: the fallback is loaded lazily, and
  // prerender waits for it where a static render would draw nothing.
  const { prelude } = await prerender(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  );
  return visibleText(await new Response(prelude).text());
}

describe('when a route fails', () => {
  it('shows the app’s fallback, with what went wrong and a way to try again', async () => {
    const text = await renderAt('/broken');
    expect(text).toContain('Something went wrong');
    expect(text).toContain('the instances could not be read');
    expect(text).toContain('Try again');
  });

  it('says an unknown address leads nowhere, and offers a way back', async () => {
    const text = await renderAt('/no-such-page');
    expect(text).toContain('This page does not exist');
    expect(text).toContain('Go to the dashboard');
  });
});
