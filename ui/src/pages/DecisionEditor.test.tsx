/**
 * The decision editor's parts, rendered.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import type { ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

import { findCoverageGaps } from '../domain/decisionCoverage';
import type { DecisionInputColumn, DecisionRuleRow } from '../domain/decisionTable';
import { CoverageCard } from './DecisionEditor';

const tier: DecisionInputColumn = { id: 'i1', label: 'Tier', expression: 'tier', type: 'string' };
const rule = (cell: string): DecisionRuleRow => ({ id: cell, input_entries: [cell], output_entries: ['x'] });
const render = (element: ReactElement) => renderToStaticMarkup(<MantineProvider>{element}</MantineProvider>);

/** Every button's accessible name, with the text it shows. */
function buttons(html: string): { name: string; text: string }[] {
  return [...html.matchAll(/<button([^>]*)>([\s\S]*?)<\/button>/g)].map(([, attributes, inner]) => ({
    name: attributes.match(/aria-label="([^"]*)"/)?.[1] ?? '',
    text: inner.replace(/<[^>]+>/g, '').trim(),
  }));
}

/**
 * WCAG 2.5.3, label in name: someone who drives the page by voice says what
 * they see. The button shows "Add line" and was named "Add a line for this
 * case: …", which contains no "Add line" to say.
 */
describe('the coverage card', () => {
  it('names each Add line button with the words it shows', () => {
    const report = findCoverageGaps([tier], [rule('"GOLD"')]);
    const addLine = buttons(render(<CoverageCard report={report} ruleCount={1} onAddLine={() => {}} />)).filter(
      (button) => button.text === 'Add line',
    );
    expect(addLine).toHaveLength(1);
    expect(addLine[0].name).toStartWith('Add line');
    expect(addLine[0].name).toContain('Nothing decides when Tier is anything else');
  });
});
