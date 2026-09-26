/**
 * Try it, rendered on its own.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { DecisionInputColumn, DecisionOutputColumn } from '../../domain/decisionTable';
import { trialOutcome } from '../../domain/decisionTrial';
import { TrialPanel, type TrialPanelProps } from './TrialPanel';

const inputs: DecisionInputColumn[] = [{ id: 'i1', label: 'Amount', expression: 'amount', type: 'number' }];
const outputs: DecisionOutputColumn[] = [{ id: 'o1', label: 'Discount', name: 'discount', type: 'number' }];

const panel = (over: Partial<TrialPanelProps>): TrialPanelProps => ({
  target: { key: 'discount', version: 3 },
  inputs,
  outputs,
  values: {},
  onValues: () => {},
  onRun: () => {},
  running: false,
  error: null,
  answer: null,
  ...over,
});

const textOf = (props: TrialPanelProps) =>
  renderToStaticMarkup(
    <MantineProvider>
      <TrialPanel {...props} />
    </MantineProvider>,
  )
    .replace(/<style[^>]*>[\s\S]*?<\/style>/g, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ');

const decided = trialOutcome({ result: { values: { discount: 5 } }, matchedRules: [0], matchedRuleIds: ['r1'] }, 'screen', 'stored');

describe('Try it', () => {
  it('names the stored version it runs', () => {
    expect(textOf(panel({}))).toContain('Runs the saved version (v3) and highlights the lines that matched.');
  });

  it('asks for a save before a table that was never saved can run', () => {
    expect(textOf(panel({ target: null }))).toContain('Save the table first: Try it runs the saved version.');
  });

  it('says an answer about the saved version is only that', () => {
    const text = textOf(panel({ answer: { outcome: decided, standing: 'saved-only', staleReason: '', lines: [0] } }));
    expect(text).toContain('Line 1 matched');
    expect(text).toContain('This is what the saved version decides. Your changes are not saved yet');
    expect(text).toContain('Discount: 5');
  });

  it('shows why an answer is gone rather than the answer', () => {
    const reason = 'The saved table has changed since this ran, so its answer is no longer shown. Run it again.';
    const text = textOf(panel({ answer: { outcome: decided, standing: 'stale', staleReason: reason, lines: [] } }));
    expect(text).toContain(reason);
    expect(text).not.toContain('Line 1 matched');
  });
});
