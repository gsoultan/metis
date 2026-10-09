/**
 * The foot of the migration dialog, rendered: what it says before an apply
 * that will be sent to somebody else, and what stays on screen after one that
 * was, or that passed instances over.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { MigrationApplyFooter } from './MigrationApplyFooter';
import { applyLabel, approvalNeeded } from '../domain/migrationApproval';
import { migrationOutcome, type MigrationReply } from '../domain/migrationOutcome';
import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { TranslationContext } from '../i18n/context';
import { format, type Catalogue, type Values } from '../i18n/translate';
import type { ApiMigrationPlan, ApiPassedOver } from '../services/types';

const plan = (over: Partial<ApiMigrationPlan> = {}): ApiMigrationPlan => ({
  source_key: 'quotation',
  source_version: 2,
  target_version: 5,
  target_id: 'def-5',
  instances: 3,
  moves: [{ from: 'opsApprove', to: 'salesApprove', tokens: 3, tasks: 3, jobs: 0, mapped: true }],
  requires_second_approver: false,
  ...over,
});

const why = [
  '“Operations approve” would be skipped for every listed instance waiting at it when the migration runs, and nobody would perform it',
  '“Credit check” is redirected to “Legal review” in a process with controls not yet passed',
];
const asks = plan({ requires_second_approver: true, second_approver_reasons: why });

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
    because: why,
  },
};

const left = (n: number): ApiPassedOver => ({
  instance_id: `0199c0de-0000-7000-8000-00000000000${n}`,
  cause: 'left_the_step',
  steps: [{ node_id: 'opsApprove', name: 'Operations approve' }],
  steps_in_all: 1,
  reason: 'It was no longer waiting at "Operations approve" when the migration reached it.',
});
const passedOver: MigrationReply = { plan: plan(), applied: true, passed_over: [left(1), left(2)], passed_over_in_all: 340 };

const at = () => '6 Oct 2026, 09:12';

interface Shown {
  reply?: MigrationReply;
  plan?: ApiMigrationPlan;
  ready?: boolean;
  catalogue?: Catalogue;
}

function render({ reply, plan: planned = plan(), ready = true, catalogue = en }: Shown = {}): string {
  const t = (key: string, values?: Values) => format(catalogue, key, values);
  return renderToStaticMarkup(
    <MantineProvider>
      <TranslationContext value={{ locale: catalogue === id ? 'id' : 'en', t, setLocale: () => {} }}>
        <MigrationApplyFooter
          outcome={reply ? migrationOutcome(reply, 5, t, at) : null}
          needed={approvalNeeded(planned, t)}
          label={applyLabel(planned, t)}
          ready={ready}
          applying={false}
          planning={false}
          onApply={() => {}}
          onClose={() => {}}
        />
      </TranslationContext>
    </MantineProvider>,
  );
}

const textOf = (html: string) =>
  html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/&quot;/g, '"').replace(/\s+/g, ' ');
const count = (html: string, part: string) => html.split(part).length - 1;
/** The text on each button, in order. */
const buttons = (html: string) => [...html.matchAll(/<button[^>]*>([\s\S]*?)<\/button>/g)].map((match) => textOf(match[1]).trim());

/**
 * What the element that announces itself holds: from its opening tag to the
 * `</div>` that closes it, found by counting, since the markup nests.
 */
function theAlert(html: string): string {
  const start = html.indexOf('role="alert"');
  const tag = /<(\/?)div\b/g;
  tag.lastIndex = start;
  let depth = 1;
  for (let found = tag.exec(html); found !== null; found = tag.exec(html)) {
    depth += found[1] === '/' ? -1 : 1;
    if (depth === 0) return html.slice(start, found.index);
  }
  return html.slice(start);
}

describe('before an apply', () => {
  it('offers to move, as it always did, for a plan one administrator can apply', () => {
    const html = render();
    expect(buttons(html)).toEqual(['Cancel', 'Move 3 instances']);
    expect(html).not.toContain('role="alert"');
    expect(textOf(html)).not.toContain('administrator');
  });

  it('says a second administrator has to approve, and why, and offers to send it', () => {
    const html = render({ plan: asks });
    const text = textOf(html);
    expect(text).toContain('A second administrator has to approve this');
    expect(text).toContain('Nothing moves until a different administrator approves it.');
    for (const reason of why) expect(text).toContain(reason);
    expect(count(html, '<li')).toBe(why.length);
    expect(buttons(html)).toEqual(['Cancel', 'Send for approval']);
    expect(text).not.toContain('Move 3 instances');
    // Announced, as the dialog's refusals and warnings are.
    expect(count(html, 'role="alert"')).toBe(1);
  });

  it('keeps the button off for a plan that is not ready, whatever it would say', () => {
    const html = render({ plan: asks, ready: false });
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>)[\s\S])*Send for approval/);
  });
});

describe('after an apply that was sent for approval', () => {
  const html = render({ reply: sentForApproval, plan: asks });
  const text = textOf(html);

  it('says it was sent, who asked, until when and why', () => {
    expect(text).toContain('Sent for approval');
    expect(text).toContain('Asked for by Dita Larasati.');
    expect(text).toContain('Nothing moves until a different administrator approves it. The request expires on 6 Oct 2026, 09:12.');
    expect(text).toContain('Why a second administrator is asked');
    for (const reason of why) expect(text).toContain(reason);
    expect(count(html, '<li')).toBe(why.length);
  });

  it('does not say that nothing was moved, or show an error, or anything a developer would say', () => {
    expect(text).not.toContain('Nothing was moved');
    expect(text).not.toContain('did not apply');
    expect(text).not.toContain('pending_approval');
    expect(text).not.toMatch(/\b202\b/);
    expect(text).not.toContain('0199c0de');
    expect(html).not.toContain('mantine-color-red');
  });

  it('leaves nothing to send twice, and closes rather than cancels', () => {
    // "Cancel" under a request just sent reads as withdrawing it, which it
    // does not do.
    expect(buttons(html)).toEqual(['Close']);
  });

  it('says it once: the plan’s own notice gives way to the request', () => {
    expect(count(text, 'A second administrator has to approve this')).toBe(0);
    expect(count(html, 'role="alert"')).toBe(1);
    expect(count(text, 'Nothing moves until a different administrator approves it.')).toBe(1);
  });

  it('announces the sentence, and leaves the reasons to be read as a list', () => {
    const announced = theAlert(html);
    expect(textOf(announced)).toContain('Sent for approval');
    expect(textOf(announced)).toContain('The request expires on 6 Oct 2026, 09:12.');
    expect(announced).not.toContain('<li');
  });

  it('reads in Indonesian', () => {
    const indonesian = render({ reply: sentForApproval, plan: asks, catalogue: id });
    expect(textOf(indonesian)).toContain('Dikirim untuk persetujuan');
    expect(textOf(indonesian)).toContain('Diminta oleh Dita Larasati.');
    expect(textOf(indonesian)).toContain('Mengapa administrator kedua diminta');
    expect(buttons(indonesian)).toEqual(['Tutup']);
  });
});

describe('after an apply that passed instances over', () => {
  const html = render({ reply: passedOver });
  const text = textOf(html);

  it('says so, and how many, from the server’s count', () => {
    expect(text).toContain('Applied, but not to every instance');
    expect(text).toContain('340 instances were not moved. The list below says why.');
    expect(text).not.toContain('now run on v5');
  });

  it('lists each instance by its reference with why, as a list', () => {
    expect(count(html, '<ul')).toBe(1);
    expect(count(html, '<li')).toBe(2);
    expect(text).toContain('Instance #000001');
    expect(text).toContain('Instance #000002');
    expect(text).toContain('It was no longer waiting at "Operations approve" when the migration reached it, so nothing was decided there.');
    expect(text).not.toContain('0199c0de');
    expect(text).not.toContain('opsApprove');
    expect(text).not.toContain('left_the_step');
  });

  it('says how many more there were than it lists', () => {
    expect(text).toContain('and 338 more instances');
  });

  it('scrolls the list inside the dialog, by keyboard too, under a name', () => {
    // A region that scrolls and cannot be focused cannot be scrolled without a
    // pointer; one that can be focused and has no name is announced as nothing.
    const region = /<div[^>]*tabindex="0"[^>]*>/.exec(html)?.[0] ?? '';
    expect(region).toContain('role="group"');
    const named = /aria-labelledby="([^"]+)"/.exec(region)?.[1] ?? 'no name';
    expect(html).toMatch(new RegExp(`<p[^>]*id="${named}"[^>]*>Instances that were not moved</p>`));
    expect(html).toContain('max-height');
  });

  it('is markup a browser will not rearrange: no paragraph inside a list item’s label', () => {
    // Mantine puts an item's children in a span, and a paragraph cannot be
    // inside one.
    const items = html.slice(html.indexOf('<ul'), html.indexOf('</ul>'));
    expect(items).toContain('<li');
    expect(items).not.toContain('<p');
  });

  it('announces the sentence and not the whole list', () => {
    // An alert is read out whole, at once. Two hundred instances read out as
    // one interruption is not an announcement.
    expect(count(html, 'role="alert"')).toBe(1);
    const announced = theAlert(html);
    expect(textOf(announced)).toContain('340 instances were not moved. The list below says why.');
    expect(announced).not.toContain('<li');
    expect(textOf(announced)).not.toContain('Instance #');
  });

  it('can be applied again for what is left, and is closed rather than cancelled', () => {
    expect(buttons(html)).toEqual(['Close', 'Move 3 instances']);
  });

  it('still says a second administrator is needed, when what is left needs one', () => {
    const both = render({ reply: passedOver, plan: asks });
    expect(textOf(both)).toContain('Applied, but not to every instance');
    expect(textOf(both)).toContain('A second administrator has to approve this');
    expect(buttons(both)).toEqual(['Close', 'Send for approval']);
  });

  it('reads in Indonesian', () => {
    const indonesian = textOf(render({ reply: passedOver, catalogue: id }));
    expect(indonesian).toContain('Diterapkan, tetapi tidak pada semua instansi');
    expect(indonesian).toContain('340 instansi tidak dipindahkan.');
    expect(indonesian).toContain('Instansi #000001');
    expect(indonesian).toContain('Instansi ini sudah tidak menunggu di "Operations approve"');
    expect(indonesian).toContain('dan 338 instansi lainnya');
  });
});
