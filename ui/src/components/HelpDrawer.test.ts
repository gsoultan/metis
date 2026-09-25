import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { createElement } from 'react';

import { standInForAppStore, userWithRoles } from '../test/appStoreStandIn';
import { renderStatic, visibleText } from '../test/renderStatic';
import { HelpTabs } from './HelpDrawer';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const PROJECT = 'project-1';

beforeEach(() => {
  store.set({ user: userWithRoles(['ADMIN']), currentProjectId: PROJECT, token: 'a-session' });
});

/** A cache holding what the server answered, under the keys the hooks ask with. */
function answered(entries: [readonly unknown[], unknown][]): QueryClient {
  const client = new QueryClient();
  for (const [key, data] of entries) client.setQueryData(key, data);
  return client;
}

const noRows = { err: '', pageInfo: undefined };

/** A project with a process, an instance, and no connection or people yet. */
const PROJECT_UNDER_WAY: [readonly unknown[], unknown][] = [
  [['definitions', PROJECT, 1, 25], { ...noRows, definitions: [{ id: 'd1', key: 'invoice' }] }],
  [['instances', PROJECT, 1, 25, '', '', false], { ...noRows, instances: [{ id: 'i1' }], statusCounts: [], needsAttentionTotal: 0, needsAttentionIds: [] }],
  [['connector-instances', PROJECT], { instances: [], err: '' }],
  [['participants', PROJECT], { participants: [], err: '' }],
];

/*
 * "Complete a task" was answered from the newest 200 tasks. A project whose
 * 200 newest tasks are all still open, and whose completed task is older, was
 * told nobody had completed one. The statistics count completed tasks across
 * the whole project.
 */
describe('Help, on whether a task has been completed', () => {
  it('reads the project statistics, not the newest page of tasks', async () => {
    const html = await renderStatic(
      createElement(HelpTabs, { onNavigate: () => {} }),
      answered([
        ...PROJECT_UNDER_WAY,
        [['tasks', PROJECT, 1, 200], { tasks: Array.from({ length: 200 }, (_, i) => ({ id: `t${i}`, status: 'unclaimed' })), pageInfo: undefined }],
        [['stats', PROJECT], { stats: { completedTasks: 1, totalTasks: 201 }, err: '' }],
      ]),
    );
    expect(visibleText(html)).toContain('Done: Complete a task');
  });
});
