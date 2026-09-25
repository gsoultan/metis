import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { createElement } from 'react';

import { standInForAppStore, userWithRoles } from '../test/appStoreStandIn';
import id from '../i18n/catalogues/id';
import { inLanguage, renderStatic, visibleText } from '../test/renderStatic';
import { GlossaryPanel } from './GlossaryPanel';
import { HelpTabs } from './HelpDrawer';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const PROJECT = 'project-1';

beforeEach(() => {
  store.set({ user: userWithRoles(['ADMIN']), currentProjectId: PROJECT, token: 'a-session' });
});

/**
 * A cache holding what the server answered, under the keys the hooks ask with.
 *
 * Not retried on mount, so a query left failed is drawn failed: a real browser
 * retries once, then shows the failure the same way.
 */
function answered(entries: [readonly unknown[], unknown][], failedKeys: (readonly unknown[])[] = []): QueryClient {
  const client = new QueryClient({ defaultOptions: { queries: { retryOnMount: false } } });
  for (const [key, data] of entries) client.setQueryData(key, data);
  for (const key of failedKeys) {
    const query = client.getQueryCache().build(client, { queryKey: key });
    query.setState({ ...query.state, status: 'error', error: new Error('The server refused'), errorUpdatedAt: Date.now() });
  }
  return client;
}

const noRows = { err: '', pageInfo: undefined };

/** A project with a process, an instance, and no connection or people yet. */
const PROJECT_UNDER_WAY: [readonly unknown[], unknown][] = [
  [['definitions', PROJECT, 1, 25], { ...noRows, definitions: [{ id: 'd1', key: 'invoice' }] }],
  [['instances', PROJECT, 1, 25, '', '', false], { ...noRows, instances: [{ id: 'i1' }], statusCounts: [], needsAttentionTotal: 0, needsAttentionIds: [] }],
  [['connector-instances', PROJECT], { instances: [], err: '' }],
  [['participants', PROJECT, { limit: 1 }], { participants: [], err: '' }],
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

/*
 * The connections and people requests throw when the server refuses. Help
 * waited for them for good, drawing every step unticked as though still
 * loading. It says what happened instead, and offers to try again.
 */
describe('Help, when a request for its progress fails', () => {
  it('says it could not check, instead of drawing a checklist', async () => {
    const withoutConnections = PROJECT_UNDER_WAY.filter(([key]) => key[0] !== 'connector-instances');
    const html = await renderStatic(
      createElement(HelpTabs, { onNavigate: () => {} }),
      answered(
        [...withoutConnections, [['stats', PROJECT], { stats: { completedTasks: 1 }, err: '' }]],
        [['connector-instances', PROJECT]],
      ),
    );
    expect(visibleText(html)).toContain("Could not check this project's progress");
    expect(visibleText(html)).not.toContain('Done:');
  });
});

/*
 * Help asks whether the project has done each thing, which is a question
 * about one row or a count. It used to download the whole people directory,
 * and the newest 200 tasks, every time it was opened.
 */
describe('what Help asks the server for', () => {
  it('asks for one person and the task counts, not the directory or a page of tasks', async () => {
    const client = answered(PROJECT_UNDER_WAY);
    await renderStatic(createElement(HelpTabs, { onNavigate: () => {} }), client);
    const asked = client.getQueryCache().getAll().map((query) => JSON.stringify(query.queryKey));
    expect(asked).toContain(JSON.stringify(['participants', PROJECT, { limit: 1 }]));
    expect(asked).not.toContain(JSON.stringify(['participants', PROJECT]));
    expect(asked).toContain(JSON.stringify(['stats', PROJECT]));
    expect(asked.some((key) => key.startsWith('["tasks"'))).toBe(false);
  });
});

/* Help's tabs, checklist and glossary follow the language the rest of the page is in. */
describe('Help in Indonesian', () => {
  it('names its tabs and draws its checklist in Indonesian', async () => {
    const html = await renderStatic(
      inLanguage(createElement(HelpTabs, { onNavigate: () => {} }), 'id', id),
      answered([...PROJECT_UNDER_WAY, [['stats', PROJECT], { stats: { completedTasks: 1 }, err: '' }]]),
    );
    const text = visibleText(html);
    expect(text).toContain('Memulai');
    expect(text).toContain('Glosarium');
    expect(text).toContain('Selesai: Terapkan sebuah proses');
    expect(text).not.toContain('Deploy a process');
  });

  /* Drawn on its own: an inactive tab's panel is empty until it is opened. */
  it('draws the glossary in Indonesian', async () => {
    const html = await renderStatic(inLanguage(createElement(GlossaryPanel), 'id', id));
    const text = visibleText(html);
    expect(html).toContain('aria-label="Cari di glosarium"');
    expect(text).toContain('Instansi');
    expect(text).toContain('istilah');
    expect(text).toContain('disebut juga Process instance');
    expect(text).not.toContain('One run of a process');
  });
});
