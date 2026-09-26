/**
 * A component rendered to HTML inside the providers every page has: Mantine, a
 * router, so a link renders the address it goes to, and a query cache.
 *
 * For tests that assert on what is drawn. A test of the domain function behind
 * a component passes just the same when the component stops calling it, or
 * calls it with the wrong thing; the markup does not.
 *
 * Nothing is fetched: a query renders from what is already in the cache it is
 * given, and effects do not run.
 */

import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { createElement, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

import { TranslationContext } from '../i18n/context';
import { format, type Catalogue } from '../i18n/translate';

export async function renderStatic(element: ReactElement, queryClient: QueryClient = new QueryClient()): Promise<string> {
  const root = createRootRoute({ component: () => element });
  const router = createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ['/'] }) });
  await router.load();
  return renderToStaticMarkup(
    createElement(
      QueryClientProvider,
      { client: queryClient },
      createElement(MantineProvider, null, createElement(RouterProvider, { router })),
    ),
  );
}

const ENTITIES: Record<string, string> = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#x27;': "'", '&#39;': "'" };

/** The markup's text as a reader sees it: no tags, no Mantine style blocks, entities decoded. */
export function visibleText(html: string): string {
  return html
    .replace(/<style[\s\S]*?<\/style>/g, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/&(?:amp|lt|gt|quot|#x27|#39);/g, (entity) => ENTITIES[entity])
    .replace(/\s+/g, ' ')
    .trim();
}

/** Every link's address, in the order they are drawn. */
export function linkTargets(html: string): string[] {
  return [...html.matchAll(/<a\b[^>]*\shref="([^"]*)"/g)].map((match) => match[1].replace(/&amp;/g, '&'));
}

/**
 * The element as it reads in another language: the same translation context
 * the provider gives the app, over the catalogue given.
 */
export function inLanguage(element: ReactElement, locale: string, catalogue: Catalogue): ReactElement {
  const value = { locale, t: (key: string, values?: Record<string, string | number>) => format(catalogue, key, values), setLocale: () => {} };
  return createElement(TranslationContext, { value }, element);
}
