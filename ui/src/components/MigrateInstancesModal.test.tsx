/**
 * The migration dialog, rendered, with its plan and its two versions stood in
 * for: what it says of a plan before anybody presses anything, and what it
 * does with each answer an apply can come back with.
 */
import { beforeEach, describe, expect, it, mock } from 'bun:test';
import { MantineProvider, Modal, createTheme } from '@mantine/core';
import { notifications, notificationsStore } from '@mantine/notifications';
import { createElement, type ComponentProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

import type { MigrationReply } from '../domain/migrationOutcome';
import type { ApiDefinition, ApiMigrationPlan } from '../services/types';

const basePlan: ApiMigrationPlan = {
  source_key: 'quotation',
  source_version: 2,
  target_version: 5,
  target_id: 'def-5',
  instances: 3,
  moves: [{ from: 'opsApprove', to: 'opsApprove', tokens: 3, tasks: 3, jobs: 0, mapped: false }],
  requires_second_approver: false,
};

// What the stand-in planner answers, and what the stand-in apply answers or
// throws; each test sets them before it renders.
let planned: ApiMigrationPlan | null = basePlan;
let answer: () => Promise<MigrationReply & { err?: string }> = async () => ({ plan: basePlan, applied: true });
// What the last apply sent and what came back, as the mutation keeps them.
let last: { variables?: unknown; data?: MigrationReply & { err?: string } } = {};
// What the dialog did: how often it asked for the plan again, closed, and
// forgot the last apply.
const did = { replans: 0, closes: 0, applies: 0, forgets: 0 };

const version = (n: number): ApiDefinition => ({
  id: `def-${n}`,
  project_id: 'p-1',
  key: 'quotation',
  name: 'Quotation',
  version: n,
  nodes: [],
  flows: [],
});

const definitionHooks = await import('../hooks/useDefinitions');
mock.module('../hooks/useDefinitions', () => ({
  ...definitionHooks,
  useDefinition: (id: string | null) => ({
    isLoading: false,
    isError: false,
    data: id ? { definition: version(id === 'def-2' ? 2 : 5) } : undefined,
  }),
  useMigrateInstances: () => ({
    mutateAsync: () => {
      did.applies += 1;
      return answer();
    },
    isPending: false,
    variables: last.variables,
    data: last.data,
    reset: () => {
      did.forgets += 1;
    },
  }),
}));
mock.module('../hooks/useMigrationPlan', () => ({
  useMigrationPlan: () => ({
    plan: planned,
    error: null,
    fresh: true,
    replan: () => {
      did.replans += 1;
    },
    reset: () => {},
  }),
}));

// The foot of the dialog as it is, with what it was handed kept: static markup
// has no button to press, so the press is made by calling what the button
// would call. The real component is taken before it is stood in for: standing
// a module in changes what its namespace answers, and a stand-in that asked
// the namespace for the component would be asking for itself.
const footer = await import('./MigrationApplyFooter');
const RealFooter = footer.MigrationApplyFooter;
type FooterProps = ComponentProps<typeof RealFooter>;
let handed: FooterProps | null = null;
mock.module('./MigrationApplyFooter', () => ({
  MigrationApplyFooter: (props: FooterProps) => {
    handed = props;
    return createElement(RealFooter, props);
  },
}));

const { MigrateInstancesModal } = await import('./MigrateInstancesModal');

// Rendered in place rather than into a portal, which static markup cannot reach.
const theme = createTheme({ components: { Modal: Modal.extend({ defaultProps: { withinPortal: false } }) } });

function render(): string {
  return renderToStaticMarkup(
    <MantineProvider theme={theme}>
      <MigrateInstancesModal
        source={{ id: 'def-2', version: 2 }}
        target={{ id: 'def-5', version: 5 }}
        processKey="quotation"
        onClose={() => {
          did.closes += 1;
        }}
      />
    </MantineProvider>,
  );
}

/** Renders the dialog and presses its apply button, then waits for the answer. */
async function apply(reply: () => Promise<MigrationReply & { err?: string }>): Promise<void> {
  answer = reply;
  handed = null;
  render();
  await (handed as FooterProps | null)?.onApply();
}

/** The toasts the dialog raised, as "title: message". */
function toasts(): string[] {
  const state = notificationsStore.getState();
  return [...state.notifications, ...state.queue].map((toast) => `${String(toast.title)}: ${String(toast.message)}`);
}

const textOf = (html: string) =>
  html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/&quot;/g, '"').replace(/\s+/g, ' ');

describe('the migration dialog, before an apply', () => {
  it('offers to move the instances of a plan one administrator can apply', () => {
    planned = basePlan;
    const text = textOf(render());
    expect(text).toContain('Move 3 instances');
    expect(text).toContain('Cancel');
    expect(text).not.toContain('Send for approval');
    expect(text).not.toContain('A second administrator has to approve this');
  });

  it('says a plan needs a second administrator, with the plan’s reasons, and offers to send it', () => {
    const reason = '“Operations approve” would be skipped for every listed instance waiting at it when the migration runs';
    planned = { ...basePlan, requires_second_approver: true, second_approver_reasons: [reason] };
    const text = textOf(render());
    expect(text).toContain('A second administrator has to approve this');
    expect(text).toContain('Nothing moves until a different administrator approves it.');
    expect(text).toContain(reason);
    expect(text).toContain('Send for approval');
    // The button no longer promises a move the press would not make. The
    // plan's own summary above still says what would move once approved.
    expect(text).not.toContain('Move 3 instances');
    expect(text).toContain('3 instances would move from version 2 to version 5');
  });
});

describe('the migration dialog, once an apply has answered', () => {
  const left = {
    instance_id: '0199c0de-0000-7000-8000-000000000001',
    cause: 'left_the_step',
    steps: [{ node_id: 'opsApprove', name: 'Operations approve' }],
    steps_in_all: 1,
    reason: 'It was no longer waiting at "Operations approve" when the migration reached it.',
  };

  beforeEach(() => {
    planned = basePlan;
    last = {};
    did.replans = 0;
    did.closes = 0;
    did.applies = 0;
    did.forgets = 0;
    notifications.clean();
    notifications.cleanQueue();
  });

  it('says a move in a toast and closes, as it always did', async () => {
    await apply(async () => ({ plan: basePlan, applied: true, passed_over: [], passed_over_in_all: 0 }));
    expect(did.applies).toBe(1);
    expect(toasts()).toEqual(['Moved to v5: 3 instances that were running on v2 now run on v5.']);
    expect(did.closes).toBe(1);
    expect(did.replans).toBe(0);
    // Closing forgets the apply: the dialog opened again starts from nothing.
    expect(did.forgets).toBe(1);
  });

  it('does not say "nothing was moved" of a request that was sent, and does not close on it', async () => {
    // The server's answer to an apply it sent to a second administrator: a 202,
    // applied: false. Read from `applied`, that was a yellow "Nothing was
    // moved" toast over a request that was waiting for somebody.
    planned = { ...basePlan, requires_second_approver: true };
    await apply(async () => ({
      plan: planned ?? basePlan,
      applied: false,
      passed_over: [],
      passed_over_in_all: 0,
      pending_approval: {
        request_id: '0199c0de-0000-7000-8000-00000000aaaa',
        status: 'pending_approval',
        requested_by: 'Dita Larasati',
        expires_at: '2026-10-06T09:12:00Z',
        because: ['“Operations approve” would be skipped'],
      },
    }));
    expect(did.applies).toBe(1);
    // Kept in the dialog to be read, so: no toast, still open, the plan that
    // was sent left as it is, and the answer not forgotten.
    expect(toasts()).toEqual([]);
    expect(did.closes).toBe(0);
    expect(did.replans).toBe(0);
    expect(did.forgets).toBe(0);
  });

  it('keeps a run that passed instances over on screen, and plans again for what is left', async () => {
    await apply(async () => ({ plan: basePlan, applied: true, passed_over: [left], passed_over_in_all: 1 }));
    expect(toasts()).toEqual([]);
    expect(did.closes).toBe(0);
    expect(did.replans).toBe(1);
  });

  it('does the same when the run passed every instance over', async () => {
    await apply(async () => ({ plan: basePlan, applied: false, passed_over: [left], passed_over_in_all: 1 }));
    expect(toasts()).toEqual([]);
    expect(did.closes).toBe(0);
    expect(did.replans).toBe(1);
  });

  it('says a request was sent in a toast when the dialog was closed before the answer came', async () => {
    // The dialog can be closed while an apply is on its way. With nothing to
    // keep the answer in, a request that was sent must not go unsaid.
    let forgottenInFlight = -1;
    await apply(async () => {
      (handed as FooterProps | null)?.onClose();
      forgottenInFlight = did.forgets;
      return {
        plan: basePlan,
        applied: false,
        passed_over: [],
        passed_over_in_all: 0,
        pending_approval: {
          request_id: '0199c0de-0000-7000-8000-00000000aaaa',
          status: 'pending_approval',
          requested_by: 'Dita Larasati',
          expires_at: '2026-10-06T09:12:00Z',
        },
      };
    });
    expect(did.closes).toBe(1);
    expect(toasts()).toHaveLength(1);
    expect(toasts()[0]).toStartWith('Sent for approval: Asked for by Dita Larasati. Nothing moves until a different administrator approves it.');
    expect(did.replans).toBe(0);
    // The apply is not let go of while it is on its way — opened again, the
    // dialog still shows it as under way — and is forgotten once it answers,
    // so the answer is not shown later as though it were the dialog's own.
    expect(forgottenInFlight).toBe(0);
    expect(did.forgets).toBe(1);
  });

  it('still says "nothing was moved" of an answer that applied nothing and asked nobody', async () => {
    await apply(async () => ({ plan: basePlan, applied: false, passed_over: [], passed_over_in_all: 0 }));
    expect(toasts()).toEqual([
      'Nothing was moved: The server worked out the plan but did not apply it, so every instance is where it was.',
    ]);
    expect(did.closes).toBe(0);
    expect(did.replans).toBe(0);
  });

  it('raises no toast for a refusal, and plans again, as it always did', async () => {
    await apply(async () => {
      throw new Error('forbidden: only an administrator may migrate running instances');
    });
    expect(toasts()).toEqual([]);
    expect(did.closes).toBe(0);
    expect(did.replans).toBe(1);
  });
});

describe('the migration dialog, with the last apply’s answer in hand', () => {
  // The request the dialog sends for these two versions with nothing edited.
  const onScreen = { source: 'def-2', target: 'def-5', mapping: {}, acknowledge: [], actions: {} };
  const asks: ApiMigrationPlan = { ...basePlan, requires_second_approver: true, second_approver_reasons: ['“Operations approve” would be skipped'] };
  const sentForApproval: MigrationReply = {
    plan: asks,
    applied: false,
    passed_over: [],
    passed_over_in_all: 0,
    pending_approval: {
      request_id: '0199c0de-0000-7000-8000-00000000aaaa',
      status: 'pending_approval',
      requested_by: 'Dita Larasati',
      expires_at: '2026-10-06T09:12:00Z',
      because: ['“Operations approve” would be skipped'],
    },
  };
  const passedOver: MigrationReply = {
    plan: basePlan,
    applied: true,
    passed_over_in_all: 1,
    passed_over: [{
      instance_id: '0199c0de-0000-7000-8000-000000000001',
      cause: 'left_the_step',
      steps: [{ node_id: 'opsApprove', name: 'Operations approve' }],
      steps_in_all: 1,
      reason: 'It was no longer waiting at "Operations approve" when the migration reached it.',
    }],
  };
  const buttons = (html: string) => [...html.matchAll(/<button[^>]*>([\s\S]*?)<\/button>/g)].map((match) => textOf(match[1]).trim());

  beforeEach(() => {
    planned = basePlan;
    last = {};
  });

  it('says a request was sent for approval, and offers nothing more to send', () => {
    planned = asks;
    last = { variables: onScreen, data: sentForApproval };
    const html = render();
    const text = textOf(html);
    expect(text).toContain('Sent for approval');
    expect(text).toContain('Asked for by Dita Larasati.');
    expect(text).toMatch(/The request expires on .*2026/);
    expect(text).toContain('Why a second administrator is asked');
    expect(text).not.toContain('Nothing was moved');
    expect(buttons(html)).toContain('Close');
    expect(buttons(html)).not.toContain('Cancel');
    expect(buttons(html)).not.toContain('Send for approval');
  });

  it('stops saying so once what is on screen is not what was sent', () => {
    // The answer was to a request with a mapping; the screen has none now.
    planned = asks;
    last = { variables: { ...onScreen, mapping: { opsApprove: 'salesApprove' } }, data: sentForApproval };
    const html = render();
    expect(textOf(html)).not.toContain('Sent for approval');
    expect(buttons(html)).toContain('Send for approval');
    expect(buttons(html)).toContain('Cancel');
  });

  it('lists the instances a run passed over, and can be applied again', () => {
    last = { variables: onScreen, data: passedOver };
    const html = render();
    const text = textOf(html);
    expect(text).toContain('Applied, but not to every instance');
    expect(text).toContain('1 instance was not moved. The list below says why.');
    expect(text).toContain('Instance #000001');
    expect(text).toContain('It was no longer waiting at "Operations approve" when the migration reached it, so nothing was decided there. It stays on v2;');
    expect(buttons(html)).toContain('Close');
    expect(buttons(html)).toContain('Move 3 instances');
  });

  it('shows nothing of an apply for another pair of versions, or of one a toast said whole, or of a refusal', () => {
    for (const kept of [
      { variables: { ...onScreen, source: 'def-3' }, data: passedOver },
      { variables: onScreen, data: { plan: basePlan, applied: true, passed_over: [], passed_over_in_all: 0 } },
      { variables: onScreen, data: { ...passedOver, err: 'invalid argument: the plan no longer holds' } },
      { variables: onScreen },
    ]) {
      last = kept;
      const html = render();
      expect(textOf(html)).not.toContain('Applied, but not to every instance');
      expect(buttons(html)).toContain('Cancel');
      expect(buttons(html)).toContain('Move 3 instances');
    }
  });
});
