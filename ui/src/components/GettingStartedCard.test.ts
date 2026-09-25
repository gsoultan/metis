import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { createElement } from 'react';

import type { GettingStartedFacts } from '../domain/gettingStarted';
import { useAppStore } from '../store/useAppStore';
import { linkTargets, renderStatic, visibleText } from '../test/renderStatic';
import { GettingStartedCard, GettingStartedTimeline } from './GettingStartedCard';

/*
 * The store, stood in for. A server render reads a zustand store's initial
 * state, never what a test sets on it, so the module is replaced for this
 * file and put back after it: bun's module mocks outlive the file otherwise.
 */
type AppState = ReturnType<typeof useAppStore.getState>;
type SignedInUser = NonNullable<AppState['user']>;

const realUseAppStore = useAppStore;
let signedIn: Partial<AppState> = {};

function useStandInStore(selector?: (state: AppState) => unknown) {
  const state = signedIn as AppState;
  return selector ? selector(state) : state;
}

mock.module('../store/useAppStore', () => ({
  useAppStore: Object.assign(useStandInStore, { getState: () => signedIn as AppState }),
}));

afterAll(() => {
  mock.module('../store/useAppStore', () => ({ useAppStore: realUseAppStore }));
});

function userWith(roles: string[]): SignedInUser {
  return { id: 'user-1', name: 'Ana', displayName: 'Ana', organization: 'Acme', username: 'ana', role: roles.join(','), roles };
}

beforeEach(() => {
  signedIn = { user: userWith(['ADMIN']), currentProjectId: 'project-1' };
});

const NOTHING_DONE: GettingStartedFacts = {
  processDeployed: false,
  instanceStarted: false,
  taskCompleted: false,
  connectionSetUp: false,
  peopleAdded: false,
};

/** The address of the card's one button-shaped link: the next step. */
function nextStepTarget(html: string): string | undefined {
  const button = /<a\b[^>]*class="[^"]*mantine-Button-root[^"]*"[^>]*>/.exec(html)?.[0] ?? '';
  return /\shref="([^"]*)"/.exec(button)?.[1]?.replace(/&amp;/g, '&');
}

/*
 * Each step links to the page where it is done. The links were Mantine
 * components given the router's Link as `component`, which types `to` as any
 * string: a route that does not exist, or one missing the search it requires
 * (Processes needs its tab), compiled. They are router links now, checked
 * against the route tree, and what they render is asserted here.
 */
describe('where each step is done', () => {
  it('is linked from every step in the timeline, as a real link', async () => {
    const html = await renderStatic(createElement(GettingStartedTimeline, { facts: NOTHING_DONE }));
    expect(linkTargets(html)).toEqual([
      '/models?tab=processes',
      '/models?tab=processes',
      '/inbox',
      '/connectors',
      '/people',
    ]);
  });

  it("is where the card's button for the next step goes", async () => {
    const html = await renderStatic(createElement(GettingStartedCard, { facts: NOTHING_DONE }));
    expect(nextStepTarget(html)).toBe('/models?tab=processes');
  });

  it('moves on with the next step', async () => {
    const html = await renderStatic(
      createElement(GettingStartedCard, { facts: { ...NOTHING_DONE, processDeployed: true, instanceStarted: true } }),
    );
    expect(nextStepTarget(html)).toBe('/inbox');
  });
});

/*
 * A step somebody cannot do is not shown to them. The server refuses deploying
 * and importing people to anybody without the designer or administrator role,
 * and setting up a connection to anybody but an administrator; a link to the
 * page where they would be refused is a dead end.
 */
describe('the steps somebody is shown', () => {
  it('are only starting an instance and completing a task, for somebody with no role', async () => {
    signedIn = { ...signedIn, user: userWith([]) };
    const html = await renderStatic(createElement(GettingStartedTimeline, { facts: NOTHING_DONE }));
    expect(linkTargets(html)).toEqual(['/models?tab=processes', '/inbox']);
  });

  it('leave out setting up a connection, for a designer', async () => {
    signedIn = { ...signedIn, user: userWith(['DESIGNER']) };
    const html = await renderStatic(createElement(GettingStartedTimeline, { facts: NOTHING_DONE }));
    expect(linkTargets(html)).toEqual(['/models?tab=processes', '/models?tab=processes', '/inbox', '/people']);
  });

  it('are counted on the card, and the card goes once they are done', async () => {
    signedIn = { ...signedIn, user: userWith([]) };
    const started = await renderStatic(
      createElement(GettingStartedCard, { facts: { ...NOTHING_DONE, instanceStarted: true } }),
    );
    expect(started).toContain('1 of 2 done');

    const finished = await renderStatic(
      createElement(GettingStartedCard, { facts: { ...NOTHING_DONE, instanceStarted: true, taskCompleted: true } }),
    );
    expect(visibleText(finished)).toBe('');
  });
});
