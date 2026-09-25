import { afterAll, beforeEach, describe, expect, it, mock } from 'bun:test';
import { createElement } from 'react';

import type { GettingStartedFacts, GettingStartedProgress } from '../domain/gettingStarted';
import { standInForAppStore, userWithRoles } from '../test/appStoreStandIn';
import { linkTargets, renderStatic, visibleText } from '../test/renderStatic';
import { GettingStartedCard, GettingStartedTimeline } from './GettingStartedCard';

const store = standInForAppStore((specifier, factory) => mock.module(specifier, factory));
afterAll(() => store.restore());

const SIGNED_IN = { user: userWithRoles(['ADMIN']), currentProjectId: 'project-1' };

beforeEach(() => {
  store.set(SIGNED_IN);
});

const NOTHING_DONE: GettingStartedFacts = {
  processDeployed: false,
  instanceStarted: false,
  taskCompleted: false,
  connectionSetUp: false,
  peopleAdded: false,
};

const known = (facts: GettingStartedFacts): GettingStartedProgress => ({ state: 'known', facts });
const noRetry = () => {};

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
    const html = await renderStatic(createElement(GettingStartedTimeline, { progress: known(NOTHING_DONE), onRetry: noRetry }));
    expect(linkTargets(html)).toEqual([
      '/models?tab=processes',
      '/models?tab=processes',
      '/inbox',
      '/connectors',
      '/people',
    ]);
  });

  it("is where the card's button for the next step goes", async () => {
    const html = await renderStatic(createElement(GettingStartedCard, { progress: known(NOTHING_DONE), onRetry: noRetry }));
    expect(nextStepTarget(html)).toBe('/models?tab=processes');
  });

  it('moves on with the next step', async () => {
    const html = await renderStatic(
      createElement(GettingStartedCard, { progress: known({ ...NOTHING_DONE, processDeployed: true, instanceStarted: true }), onRetry: noRetry }),
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
    store.set({ ...SIGNED_IN, user: userWithRoles([]) });
    const html = await renderStatic(createElement(GettingStartedTimeline, { progress: known(NOTHING_DONE), onRetry: noRetry }));
    expect(linkTargets(html)).toEqual(['/models?tab=processes', '/inbox']);
  });

  it('leave out setting up a connection, for a designer', async () => {
    store.set({ ...SIGNED_IN, user: userWithRoles(['DESIGNER']) });
    const html = await renderStatic(createElement(GettingStartedTimeline, { progress: known(NOTHING_DONE), onRetry: noRetry }));
    expect(linkTargets(html)).toEqual(['/models?tab=processes', '/models?tab=processes', '/inbox', '/people']);
  });

  it('are counted on the card, and the card goes once they are done', async () => {
    store.set({ ...SIGNED_IN, user: userWithRoles([]) });
    const started = await renderStatic(
      createElement(GettingStartedCard, { progress: known({ ...NOTHING_DONE, instanceStarted: true }), onRetry: noRetry }),
    );
    expect(started).toContain('1 of 2 done');

    const finished = await renderStatic(
      createElement(GettingStartedCard, { progress: known({ ...NOTHING_DONE, instanceStarted: true, taskCompleted: true }), onRetry: noRetry }),
    );
    expect(visibleText(finished)).toBe('');
  });
});

/*
 * When a request fails, what has been done is not known, and a checklist
 * drawn anyway says "not done" about steps that may well be done. Nor does it
 * stay blank as though still loading. It says so, and offers to try again.
 */
describe('a checklist that could not be checked', () => {
  const FAILED: GettingStartedProgress = { state: 'failed' };

  it('is not drawn on the card; the card says so and offers to try again', async () => {
    const html = await renderStatic(createElement(GettingStartedCard, { progress: FAILED, onRetry: noRetry }));
    expect(visibleText(html)).toContain("Could not check this project's progress");
    expect(visibleText(html)).toContain('Try again');
    expect(linkTargets(html)).toEqual([]);
  });

  it('is not drawn in Help either', async () => {
    const html = await renderStatic(createElement(GettingStartedTimeline, { progress: FAILED, onRetry: noRetry }));
    expect(visibleText(html)).toContain("Could not check this project's progress");
    expect(visibleText(html)).toContain('Try again');
    expect(linkTargets(html)).toEqual([]);
  });
});
