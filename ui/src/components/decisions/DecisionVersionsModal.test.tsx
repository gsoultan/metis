/**
 * A decision's version history, rendered, with its reads stood in for.
 *
 * What is tested is which control each version is offered, because each one is
 * a promise about what the server will do: the live version is not offered
 * "Make live" (it already is) or "Delete" (the server refuses it while other
 * versions remain), an older version is offered as a rollback, and a newer one
 * as making it live.
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider, Modal, createTheme } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { ApiDecisionVersion } from '../../services/types';

let versions: ApiDecisionVersion[] = [];

const decisionHooks = await import('../../hooks/useDecisions');
mock.module('../../hooks/useDecisions', () => ({
  ...decisionHooks,
  useDecisionVersions: () => ({ data: versions, isLoading: false, error: null }),
  usePromoteDecisionVersion: () => ({ mutateAsync: async () => ({}), isPending: false }),
  useDecisionImpact: () => ({ data: undefined, isLoading: false }),
  useDeleteDecision: () => ({ mutateAsync: async () => ({}), isPending: false }),
}));

const { DecisionVersionsModal } = await import('./DecisionVersionsModal');

// Rendered in place rather than into a portal, which static markup cannot reach.
const theme = createTheme({ components: { Modal: Modal.extend({ defaultProps: { withinPortal: false } }) } });

const version = (n: number, live = false): ApiDecisionVersion => ({ id: `dec-${n}`, key: 'discount', name: 'Discount', version: n, live });

function render(openId?: string): string {
  return renderToStaticMarkup(
    <MantineProvider theme={theme}>
      <DecisionVersionsModal decisionKey="discount" name="Discount" openId={openId} onClose={() => {}} onOpen={() => {}} />
    </MantineProvider>,
  );
}

/** The text of the table row for version n. */
function rowOf(html: string, n: number): string {
  const row = html.split('<tr').find((part) => part.includes(`>v${n}<`)) ?? '';
  return row.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
}

describe('a decision version history', () => {
  it('offers each version what can be done with it', () => {
    versions = [version(3), version(2, true), version(1)];
    const html = render('dec-2');

    const staged = rowOf(html, 3);
    expect(staged).toContain('Staged');
    expect(staged).toContain('Make live');
    expect(staged).toContain('Delete');

    const live = rowOf(html, 2);
    expect(live).toContain('Live');
    expect(live).toContain('Open now');
    expect(live).not.toContain('Make live');
    expect(live).not.toContain('Roll back');
    expect(live).not.toContain('Delete');

    const earlier = rowOf(html, 1);
    expect(earlier).toContain('Earlier');
    expect(earlier).toContain('Roll back');
    expect(earlier).toContain('Delete');
  });

  it('offers the only version for deletion, which deletes the decision', () => {
    versions = [version(1, true)];
    expect(rowOf(render(), 1)).toContain('Delete');
  });
});
