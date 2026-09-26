import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

/**
 * A component's HTML as the server renders it, inside the providers every
 * screen has.
 *
 * Nothing is fetched while rendering on the server, so every query reads as
 * still loading. A fresh query client each time keeps one render's cache out of
 * the next.
 */
export function renderMarkup(element: ReactElement): string {
  const client = new QueryClient();
  return renderToStaticMarkup(
    createElement(QueryClientProvider, { client }, createElement(MantineProvider, { children: element })),
  );
}
