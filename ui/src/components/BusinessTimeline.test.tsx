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

/**
 * A hand-over read "Task "Approve the refund" was assigned to citra": the
 * person it went to, and nobody who did it. The entry says who now, and the
 * timeline tells it from the entry in the reader's language.
 */
describe('a hand-over on the business timeline', () => {
  const handedOver: ApiAuditEntry[] = [
    {
      id: 'h1',
      type: 'task_assigned',
      message: '',
      narrative: 'ana reassigned task "Approve the refund" from budi to citra: budi is on leave',
      timestamp: '2026-09-28T10:00:00Z',
      node: { id: 'approve', name: 'Approve the refund' },
      data: { actor: 'ana', target: 'citra', previous_holder: 'budi', reason: 'budi is on leave' },
    },
    {
      // Written before the trail kept who acted: only the stored sentence.
      id: 'h0',
      type: 'task_assigned',
      message: '',
      narrative: 'Task "Approve the refund" was assigned to budi',
      timestamp: '2026-09-27T10:00:00Z',
      node: { id: 'approve', name: 'Approve the refund' },
      data: { actor: 'budi' },
    },
  ];

  it('says who reassigned it, from whom, to whom and why', () => {
    shown = handedOver;
    const text = textOf(renderToStaticMarkup(<MantineProvider><BusinessTimeline instanceId="i1" /></MantineProvider>));
    expect(text).toContain('ana reassigned &quot;Approve the refund&quot; from budi to citra: budi is on leave');
    expect(text).toContain('Task &quot;Approve the refund&quot; was assigned to budi');
  });

  it('renders a reason as text, never as markup', () => {
    shown = [{ ...handedOver[0], data: { ...handedOver[0].data, reason: '<img src=x onerror=alert(1)>' } }];
    const html = renderToStaticMarkup(<MantineProvider><BusinessTimeline instanceId="i1" /></MantineProvider>);
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
  });

  it('says it in Indonesian to somebody reading in Indonesian', async () => {
    shown = handedOver;
    const { TranslationContext } = await import('../i18n/context');
    const { format } = await import('../i18n/translate');
    const indonesian = (await import('../i18n/catalogues/id')).default;
    const text = textOf(renderToStaticMarkup(
      <MantineProvider>
        <TranslationContext value={{ locale: 'id', setLocale: () => {}, t: (key, values) => format(indonesian, key, values) }}>
          <BusinessTimeline instanceId="i1" />
        </TranslationContext>
      </MantineProvider>,
    ));
    expect(text).toContain('ana mengalihkan &quot;Approve the refund&quot; dari budi kepada citra: budi is on leave');
    expect(text).toContain('Task &quot;Approve the refund&quot; was assigned to budi');
  });
});
