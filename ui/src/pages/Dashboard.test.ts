import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { createElement } from 'react';

import { standInForAppStore, userWithRoles } from '../test/appStoreStandIn';
import { linkTargets, renderStatic, visibleText } from '../test/renderStatic';
import { Dashboard } from './Dashboard';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const PROJECT = 'project-1';

beforeEach(() => {
  store.set({
    user: userWithRoles(['ADMIN']),
    currentProjectId: PROJECT,
    currentOrganizationId: 'org-1',
    token: 'a-session',
  });
});

/** The answers a project set up a moment ago gives, under the keys the Dashboard and the card ask with. */
function freshProject(): QueryClient {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  const noPage = { err: '', pageInfo: undefined };
  client.setQueryData(['definitions', PROJECT, 1, 25], { ...noPage, definitions: [] });
  client.setQueryData(['instances', PROJECT, 1, 25, '', '', false], {
    ...noPage, instances: [], statusCounts: [], needsAttentionTotal: 0, needsAttentionIds: [],
  });
  client.setQueryData(['stats', PROJECT], { stats: { activeInstances: 0, totalTasks: 0, completedTasks: 0 }, err: '' });
  client.setQueryData(['connector-instances', PROJECT], { instances: [], err: '' });
  client.setQueryData(['participants', PROJECT, { limit: 1 }], { participants: [], err: '' });
  return client;
}

/*
 * The getting-started card was written, and Help drew its steps, but nothing
 * put the card where somebody new lands: the Dashboard.
 */
describe('the Dashboard', () => {
  it('shows somebody new where to start, and links the first step', async () => {
    const html = await renderStatic(createElement(Dashboard), freshProject());
    const text = visibleText(html);
    expect(text).toContain('Getting started');
    expect(text).toContain('0 of 5 done');
    expect(linkTargets(html)).toContain('/models?tab=processes');
  });

  it('shows nothing of it while the project’s answers are still on their way', async () => {
    const html = await renderStatic(createElement(Dashboard), new QueryClient());
    expect(visibleText(html)).not.toContain('Getting started');
  });
});
