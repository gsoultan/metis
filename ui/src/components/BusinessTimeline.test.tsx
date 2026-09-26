/**
 * The business timeline, rendered with an instance's audit entries stood in for.
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiAuditEntry } from '../services/types';

const decided: ApiAuditEntry[] = [
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

/**
 * One step's entries, as the server sends them: oldest first, in the order the
 * step wrote them. They were written in one transaction, so they share its
 * timestamp.
 */
const oneStep: ApiAuditEntry[] = ['Claim checked', 'Claim approved', 'Claim filed'].map((name, i) => ({
  id: `s${i}`,
  type: 'NodeReached',
  message: `Reached node: step-${i}`,
  narrative: `Process reached the '${name}' step.`,
  timestamp: '2026-09-25T10:00:00.123456Z',
  node: { id: `step-${i}`, name },
}));

// What the stand-in hook returns; each describe sets it before it renders.
let shown = decided;

// A module stood in for stays stood in for the rest of the run, so everything
// else it exports stays as it is.
const instanceHooks = await import('../hooks/useInstances');
mock.module('../hooks/useInstances', () => ({
  ...instanceHooks,
  useAuditLogs: () => ({ data: { entries: shown }, isLoading: false }),
}));

const { BusinessTimeline } = await import('./BusinessTimeline');

const textOf = (html: string) => html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

/**
 * decisionNarrative was written to say a decision in words, and the timeline
 * never used it: a decision read as `expense_approval_level v3`, `rule rule_1`
 * and `{"approval_level":"director"}` — the audit record, verbatim.
 */
describe('a decision on the business timeline', () => {
  shown = decided;
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

/**
 * The timeline reads newest first. It sorted by timestamp to get there, and the
 * entries one step writes share one, so a stable sort left them oldest first:
 * the step the instance ended on sat under the ones that led to it. The server
 * sends the trail oldest first in the order it was written, so the newest
 * first is that order reversed.
 */
describe('one step on the business timeline', () => {
  shown = oneStep;
  const text = textOf(renderToStaticMarkup(<MantineProvider><BusinessTimeline instanceId="i1" /></MantineProvider>));

  it('shows the last thing the step did at the top', () => {
    const filed = text.indexOf('Claim filed');
    const approved = text.indexOf('Claim approved');
    const checked = text.indexOf('Claim checked');
    expect(filed).toBeGreaterThanOrEqual(0);
    expect(filed).toBeLessThan(approved);
    expect(approved).toBeLessThan(checked);
  });
});
