/**
 * The decision list page, rendered, with its reads stood in for.
 *
 * What is tested here is the wiring the domain tests cannot see: which read
 * feeds which part of the page. The list shows the page the server sent and
 * says where it sits in the whole; the graph is drawn from every decision key,
 * not from the page on screen; and while the keys are loading, or when they
 * could not be loaded, the graph says so rather than drawing nothing.
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { CollectedPages } from '../domain/allPages';
import type { ApiDecisionSummary } from '../services/types';

interface Stage {
  listCalls: unknown[][];
  rows: ApiDecisionSummary[];
  total: number;
  summaries: { data?: CollectedPages<ApiDecisionSummary>; isLoading: boolean; isError: boolean };
}

const stage: Stage = { listCalls: [], rows: [], total: 0, summaries: { isLoading: false, isError: false } };

// The rest of the module stays as it is, for any other test file that imports
// it: a module stood in for here stays stood in for the rest of the run.
const decisionHooks = await import('../hooks/useDecisions');
mock.module('../hooks/useDecisions', () => ({
  ...decisionHooks,
  useDecisions: (...args: unknown[]) => {
    stage.listCalls.push(args);
    return {
      data: { summaries: stage.rows, pageInfo: { total: stage.total, page: 1, pageSize: 25, hasMore: stage.total > 25 } },
      isLoading: false,
      error: null,
      refetch: () => {},
    };
  },
  useDecisionSummaries: () => stage.summaries,
  useDecisionImpact: () => ({ data: undefined, isLoading: false }),
  useDeleteDecision: () => ({ mutateAsync: async () => {}, isPending: false }),
}));
// Everything the router exports stays as it is — other test files import it —
// but navigating goes nowhere.
const router = await import('@tanstack/react-router');
mock.module('@tanstack/react-router', () => ({ ...router, useNavigate: () => () => {} }));

const { DecisionList } = await import('./DecisionList');

const row = (i: number, live = 1, newest = live): ApiDecisionSummary => ({
  id: `id-${i}`,
  key: `d${i}`,
  name: `Decision ${i}`,
  version: live || newest,
  hit_policy: 'FIRST',
  live_version: live,
  newest_version: newest,
});
const summary = (i: number, requires: string[] = []): ApiDecisionSummary => ({
  id: `id-${i}`,
  key: `d${i}`,
  name: `Decision ${i}`,
  version: 1,
  required_decisions: requires,
});

function render(): string {
  stage.listCalls = [];
  return renderToStaticMarkup(
    <MantineProvider>
      <DecisionList onEdit={() => {}} hideHeader />
    </MantineProvider>,
  );
}

/** The markup as text, so an assertion reads the way the page does. */
const textOf = (html: string) => html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

describe('the decision list page', () => {
  it('asks the server for one page, and says where it sits among all of them', () => {
    stage.rows = Array.from({ length: 25 }, (_, i) => row(i));
    stage.total = 60;
    stage.summaries = { data: { items: [], truncated: false, total: 0 }, isLoading: false, isError: false };

    const text = textOf(render());
    expect(stage.listCalls[0]).toEqual([1, 25, '']);
    expect(text).toContain('1–25 of 60');
    expect(text).toContain('Decision 24');
    expect(text).not.toContain('Decision 25');
  });

  it('draws the graph from every decision key, not from the page on screen', () => {
    // The page holds d0..d24. d29, past it, requires d28.
    stage.rows = Array.from({ length: 25 }, (_, i) => row(i));
    stage.total = 30;
    const keys = Array.from({ length: 30 }, (_, i) => summary(i, i === 29 ? ['d28'] : []));
    stage.summaries = { data: { items: keys, truncated: false, total: 30 }, isLoading: false, isError: false };

    const text = textOf(render());
    expect(text).toContain('Then, using the above');
    expect(text).toContain('Decision 29');
    expect(text).not.toContain('nothing here answers to');
    expect(text).not.toContain('No decision depends on another');
  });

  it('says the graph is loading, or could not be loaded, while the list is shown', () => {
    stage.rows = [row(0)];
    stage.total = 1;
    stage.summaries = { isLoading: true, isError: false };
    expect(textOf(render())).toContain('Loading the decisions…');

    stage.summaries = { isLoading: false, isError: true };
    const failed = textOf(render());
    expect(failed).toContain('The decisions could not be loaded, so how they fit together cannot be shown.');
    expect(failed).toContain('Decision 0');
  });

  it('shows each decision once, with the version in force and the one waiting behind it', () => {
    stage.rows = [row(0, 2, 3), row(1, 1, 1)];
    stage.total = 2;
    stage.summaries = { data: { items: [], truncated: false, total: 0 }, isLoading: false, isError: false };

    const text = textOf(render());
    expect(text).toContain('v2 live');
    expect(text).toContain('v3 staged');
    expect(text).toContain('v1 live');
    expect(text.match(/Decision 0/g)?.length).toBe(1);
  });

  it('offers each decision its version history, where its versions are deleted one at a time', () => {
    stage.rows = [row(0, 2, 3)];
    stage.total = 1;
    stage.summaries = { data: { items: [], truncated: false, total: 0 }, isLoading: false, isError: false };

    const html = render();
    expect(html).toContain('aria-label="Version history of Decision 0"');
    // Deleting the row's version would delete the live one out from under the
    // other two; a version is deleted from the history, which says which.
    expect(html).not.toContain('aria-label="Delete Decision 0"');
  });

  it('says so when no version of a decision is live', () => {
    stage.rows = [row(0, 0, 1)];
    stage.total = 1;
    stage.summaries = { data: { items: [], truncated: false, total: 0 }, isLoading: false, isError: false };

    const text = textOf(render());
    expect(text).toContain('Nothing live');
    expect(text).toContain('v1 staged');
  });

  it('counts decisions, not versions of them, when the graph stops short', () => {
    stage.rows = [row(0)];
    stage.total = 1;
    const keys = Array.from({ length: 1000 }, (_, i) => summary(i, i === 1 ? ['d0'] : []));
    stage.summaries = { data: { items: keys, truncated: true, total: 1200 }, isLoading: false, isError: false };
    expect(textOf(render())).toContain('This project has 1200 decisions and the graph shows the first 1000 of them');
  });
});
