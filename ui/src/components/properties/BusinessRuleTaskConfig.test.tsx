/**
 * The step that asks a decision table, rendered with its read stood in for.
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { CollectedPages } from '../../domain/allPages';
import type { ApiDecision, ApiDecisionSummary } from '../../services/types';

const version = (key: string, name: string, v: number): ApiDecision => ({ id: `${key}-${v}`, key, name, version: v, hit_policy: 'FIRST' });
const keyOf = (key: string, name: string): ApiDecisionSummary => ({ id: `${key}-latest`, key, name, version: 2 });

// The first page of the list the step used to read: 25 rows, two of them
// versions of one decision.
const firstPage = [version('credit-band', 'Credit band', 1), version('credit-band', 'Credit band', 2)];
for (let i = 0; i < 23; i += 1) firstPage.push(version(`d${i}`, `Decision ${i}`, 1));
// Every key of the project, once each: the 25 above and five more.
const everyKey: CollectedPages<ApiDecisionSummary> = {
  items: [keyOf('credit-band', 'Credit band'), ...Array.from({ length: 29 }, (_, i) => keyOf(`d${i}`, `Decision ${i}`))],
  truncated: false,
  total: 30,
};

const decisionHooks = await import('../../hooks/useDecisions');
mock.module('../../hooks/useDecisions', () => ({
  ...decisionHooks,
  useDecisions: () => ({ data: { decisions: firstPage, pageInfo: { total: 26, page: 1, pageSize: 25, hasMore: true } } }),
  useDecisionSummaries: () => ({ data: everyKey, isLoading: false, isError: false }),
}));

const { BusinessRuleTaskConfig } = await import('./BusinessRuleTaskConfig');

/** What the "Decision table" picker shows for a step naming decisionKey. */
function pickerShows(decisionKey: string): string {
  const html = renderToStaticMarkup(
    <MantineProvider>
      <BusinessRuleTaskConfig data={{ decision_key: decisionKey }} onUpdate={() => {}} />
    </MantineProvider>,
  );
  const picker = html.slice(html.indexOf('>Decision table</label>'));
  return picker.match(/<input[^>]*role="combobox"[^>]*value="([^"]*)"/)?.[1] ?? '';
}

/**
 * The picker listed page 1 of the decision list — 25 rows, one per version —
 * so a decision saved twice was offered twice, and Mantine's Select refuses
 * that outright: the property panel failed with "Duplicate options are not
 * supported". A decision past the first 25 rows could not be chosen at all.
 */
describe('the decision a step asks', () => {
  it('is offered once, however many versions it has', () => {
    expect(pickerShows('credit-band')).toBe('Credit band');
  });

  it('can be any decision in the project, not only one of the first 25', () => {
    expect(pickerShows('d28')).toBe('Decision 28');
  });

  it('stays shown when it is no longer in the project', () => {
    expect(pickerShows('retired')).toBe('retired');
  });
});
