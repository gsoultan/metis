/**
 * The decision editor page, rendered, with its reads and writes stood in for.
 *
 * What is tested here is the page's own wiring, which the domain tests cannot
 * see: which table Try it runs. It must run the stored table the editor
 * loaded, at the version it loaded — never the key being typed into the Key
 * box, which may name another table or none, and never "the newest version".
 */
import { describe, expect, it, mock } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import type { TrialPanelProps } from '../components/decisions/TrialPanel';
import type { ApiDecision } from '../services/types';

const stored: ApiDecision = {
  id: 'dec-1',
  key: 'discount',
  name: 'Discount',
  version: 3,
  hit_policy: 'UNIQUE',
  inputs: [{ id: 'i1', label: 'Amount', expression: 'amount', type: 'number' }],
  outputs: [{ id: 'o1', label: 'Discount', name: 'discount', type: 'number' }],
  rules: [{ id: 'r1', inputs: ['> 10'], outputs: [5] }],
};

const sent: unknown[] = [];

// A module stood in for stays stood in for the rest of the run, so each keeps
// everything else it exports.
const decisionHooks = await import('../hooks/useDecisions');
mock.module('../hooks/useDecisions', () => ({
  ...decisionHooks,
  useDecision: () => ({ data: { decision: stored } }),
  // v4 is staged beside the live v3 the editor has open.
  useDecisionVersions: () => ({
    data: [
      { id: 'dec-4', key: 'discount', name: 'Discount', version: 4, live: false },
      { id: 'dec-1', key: 'discount', name: 'Discount', version: 3, live: true },
    ],
    isLoading: false,
    error: null,
  }),
  usePromoteDecisionVersion: () => ({ mutateAsync: async () => ({}), isPending: false }),
  useDeleteDecision: () => ({ mutateAsync: async () => ({}), isPending: false }),
  useDecisionImpact: () => ({ data: undefined }),
  useCreateDecision: () => ({ mutateAsync: async () => ({}), isPending: false }),
  useUpdateDecision: () => ({ mutateAsync: async () => ({}), isPending: false }),
  useRunDecisionTests: () => ({ mutateAsync: async () => [], isPending: false }),
  useEvaluateDecision: () => ({
    mutateAsync: async (request: unknown) => {
      sent.push(request);
      return { result: { values: {} }, matchedRules: [], matchedRuleIds: [] };
    },
  }),
}));

// Opened from the creation wizard's link, with a key typed there that is not
// the stored table's.
const router = await import('@tanstack/react-router');
mock.module('@tanstack/react-router', () => ({
  ...router,
  useNavigate: () => () => {},
  useSearch: () => ({ key: 'typed_key', name: 'Typed name' }),
}));

// The panel renders as it does in the app; the test keeps what the page gave it.
const trialPanel = await import('../components/decisions/TrialPanel');
const RealTrialPanel = trialPanel.TrialPanel;
let given: TrialPanelProps | undefined;
mock.module('../components/decisions/TrialPanel', () => ({
  ...trialPanel,
  TrialPanel: (props: TrialPanelProps) => {
    given = props;
    return <RealTrialPanel {...props} />;
  },
}));

const { DecisionEditor } = await import('./DecisionEditor');

function render(): string {
  given = undefined;
  return renderToStaticMarkup(
    <MantineProvider>
      <DecisionEditor definitionId="dec-1" />
    </MantineProvider>,
  );
}

describe('Try it on the decision editor page', () => {
  it('runs the stored table at the version loaded, not the key typed into the editor', async () => {
    const html = render();
    expect(html).toContain('Runs the saved version (v3)');
    expect(given?.target).toEqual({ key: 'discount', version: 3 });

    sent.length = 0;
    await given?.onRun();
    expect(sent).toEqual([{ key: 'discount', version: 3, variables: {} }]);
  });
});

describe('the hint under the grid', () => {
  it('says a condition can compare with another one by naming it', () => {
    expect(render()).toContain('To compare with another condition, write its name');
  });
});

describe('which version the editor has open', () => {
  it('says the open version is the live one, and offers the others', () => {
    const html = render();
    expect(html).toContain('v3');
    expect(html).toContain('>Live<');
    expect(html).toContain('aria-label="Versions of Discount"');
  });
});
