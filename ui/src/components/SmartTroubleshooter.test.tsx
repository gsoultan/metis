/**
 * The property panel's suggestions for one step, rendered.
 *
 * It made its own check for a step that asks a person and names nobody, apart
 * from the validation panel's, and the two had drifted: it read only the
 * assignee and the candidate users, so a step offered to a team was told it
 * named nobody. It says what the validation panel says now, from the same
 * check.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import type { Node } from '@xyflow/react';
import { renderToStaticMarkup } from 'react-dom/server';

import { visibleText } from '../test/renderStatic';
import type { BPMNNodeData } from '../types/bpmn';
import { SmartTroubleshooter } from './SmartTroubleshooter';

function suggestionsFor(data: Record<string, unknown>): string {
  const node = { id: 'a', type: 'userTask', position: { x: 0, y: 0 }, data: { label: 'Approve the refund', ...data } } as Node<BPMNNodeData>;
  const html = renderToStaticMarkup(
    <MantineProvider>
      <SmartTroubleshooter node={node} />
    </MantineProvider>,
  );
  return visibleText(html);
}

describe('the suggestions for a step that asks a person', () => {
  it('say who will be able to take it when it names nobody', () => {
    expect(suggestionsFor({})).toContain(
      '"Approve the refund" does not say who does it, so only administrators and operators will be able to take it.',
    );
  });

  it('are content with a step offered to a team', () => {
    const text = suggestionsFor({ candidateGroups: ['finance'] });
    expect(text).not.toContain('does not say who does it');
    expect(text).not.toContain('No assignee');
    expect(text).toContain('No configuration issues detected');
  });
});
