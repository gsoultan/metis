import { describe, expect, it } from 'bun:test';
import { createElement } from 'react';

import type { GettingStartedFacts } from '../domain/gettingStarted';
import { linkTargets, renderStatic } from '../test/renderStatic';
import { GettingStartedCard, GettingStartedTimeline } from './GettingStartedCard';

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
