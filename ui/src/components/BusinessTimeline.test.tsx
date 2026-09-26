/**
 * The business timeline, rendered with an instance's audit entries stood in for.
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiAuditEntry } from '../services/types';

const entries: ApiAuditEntry[] = [
  {
    id: 'a1',
    type: 'decision_evaluated',
    message: 'Expense approval level (v3) applied rule 1',
    timestamp: '2026-09-25T10:00:00Z',
    node: { id: 'Task_Decide', name: 'Decide the approval level' },
    data: {
      decision_key: 'expense_approval_level',
      decision_name: 'Expense approval level',
      decision_version: 3,
      matched_rules: [0],
      matched_rule_ids: ['rule_1'],
      outputs: { approval_level: 'director' },
    },
  },
];

// A module stood in for stays stood in for the rest of the run, so everything
// else it exports stays as it is.
const instanceHooks = await import('../hooks/useInstances');
mock.module('../hooks/useInstances', () => ({
  ...instanceHooks,
  useAuditLogs: () => ({ data: { entries }, isLoading: false }),
}));

const { BusinessTimeline } = await import('./BusinessTimeline');

const textOf = (html: string) => html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

/**
 * decisionNarrative was written to say a decision in words, and the timeline
 * never used it: a decision read as `expense_approval_level v3`, `rule rule_1`
 * and `{"approval_level":"director"}` — the audit record, verbatim.
 */
describe('a decision on the business timeline', () => {
  const text = textOf(renderToStaticMarkup(<MantineProvider><BusinessTimeline instanceId="i1" /></MantineProvider>));

  it('says what was decided, and by which version of which policy', () => {
    expect(text).toContain('Decided by Expense approval level: Approval level: director');
    expect(text).toContain('Policy version 3');
  });

  it('does not show the audit record verbatim', () => {
    expect(text).not.toContain('expense_approval_level v3');
    expect(text).not.toContain('rule_1');
    expect(text).not.toContain('&quot;approval_level&quot;');
  });
});
