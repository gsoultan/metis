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
  html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/&quot;/g, '"').replace(/&#x27;/g, "'").replace(/\s+/g, ' ');
const count = (html: string, part: string) => html.split(part).length - 1;
const allButtons = (html: string) => [...html.matchAll(/<button([^>]*)>([\s\S]*?)<\/button>/g)];
/** The text on each button somebody can see and press, in order. */
const buttons = (html: string) => allButtons(html).filter((match) => !match[1].includes('visibility:hidden')).map((match) => textOf(match[2]).trim());
/** The opening tags of buttons kept in the row for their place, and hidden. */
const hiddenButtons = (html: string) => allButtons(html).filter((match) => match[1].includes('visibility:hidden')).map((match) => match[1]);
/** Where a button's left edge is decided: how many buttons are laid out before it. */
const placeOf = (html: string, label: string) => allButtons(html).findIndex((match) => textOf(match[2]).trim() === label);

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
    expect(text).toContain('Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization has been set up as having one administrator.');
    expect(text).not.toContain('different administrator');
    for (const reason of why) expect(text).toContain(reason);
    expect(count(html, '<li')).toBe(why.length);
    expect(buttons(html)).toEqual(['Cancel', 'Send for approval']);
    expect(hiddenButtons(html)).toEqual([]);
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

  it('says it was sent, who asked, the rule, until when and why', () => {
    expect(text).toContain('Sent for approval');
    expect(text).toContain('Asked for by Dita Larasati.');
    expect(text).toContain(
      'Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization has been set up as having one administrator. ' +
        'The request expires on 6 Oct 2026, 09:12.',
    );
    expect(text).not.toContain('different administrator');
    expect(text).toContain('Why a second administrator is asked');
    for (const reason of why) expect(text).toContain(reason);
    expect(count(html, '<li')).toBe(why.length);
  });

  it('says how it is approved, there being no screen, and the request’s reference — which one click selects', () => {
    expect(text).toContain('There is no screen for this yet: an administrator approves or rejects it through the API.');
    expect(text).toMatch(/The request's reference is\s+0199c0de-0000-7000-8000-00000000aaaa\s*\./);
    // The reference is an element of its own, so it can be selected without
    // the sentence round it.
    const own = /<span[^>]*style="([^"]*)"[^>]*>0199c0de-0000-7000-8000-00000000aaaa<\/span>/.exec(html)?.[1] ?? '';
    expect(own).toContain('user-select:all');
    expect(count(html, '0199c0de-0000-7000-8000-00000000aaaa')).toBe(1);
  });

  it('does not say that nothing was moved, or show an error, or anything a developer would say', () => {
    expect(text).not.toContain('Nothing was moved');
    expect(text).not.toContain('did not apply');
    expect(text).not.toContain('pending_approval');
    expect(text).not.toMatch(/\b202\b/);
    expect(html).not.toContain('mantine-color-red');
  });

  it('leaves nothing to send twice, and closes rather than cancels', () => {
    // "Cancel" under a request just sent reads as withdrawing it, which it
    // does not do.
    expect(buttons(html)).toEqual(['Close']);
  });

  it('keeps Close where Cancel was, so a second click of the press lands on nothing', () => {
    // The press that sent the request is often a double click. With the apply
    // button gone and Close moved into its place, the second click closed the
    // dialog — and with it the only place the request was said. The apply
    // button keeps its place in the row, unseen and unpressable.
    const before = render({ plan: asks });
    expect(placeOf(html, 'Close')).toBe(placeOf(before, 'Cancel'));
    expect(placeOf(html, 'Send for approval')).toBe(placeOf(before, 'Send for approval'));
    const kept = hiddenButtons(html);
    expect(kept).toHaveLength(1);
    expect(kept[0]).toContain('disabled=""');
    expect(kept[0]).toContain('aria-hidden="true"');
    expect(kept[0]).toContain('tabindex="-1"');
  });

  it('puts focus on the message, which a held key cannot press', () => {
    // Focus has to go somewhere when the pressed button goes. On Close, a key
    // still held from the press would close the dialog. The message can take
    // focus and does nothing when a key is pressed on it.
    const alert = /<div[^>]*role="alert"[^>]*>/.exec(html)?.[0] ?? '';
    expect(alert).toContain('tabindex="-1"');
    expect(html).not.toContain('autofocus');
    expect(html).not.toContain('data-autofocus');
  });

  it('says it once: the plan’s own notice gives way to the request', () => {
    expect(count(text, 'A second administrator has to approve this')).toBe(0);
    expect(count(html, 'role="alert"')).toBe(1);
    expect(count(text, 'Nothing moves until it is approved.')).toBe(1);
  });

  it('announces the sentences, and leaves the reasons to be read as a list', () => {
    const announced = theAlert(html);
    expect(textOf(announced)).toContain('Sent for approval');
    expect(textOf(announced)).toContain('The request expires on 6 Oct 2026, 09:12.');
    expect(textOf(announced)).toContain('through the API');
    expect(announced).toContain('0199c0de-0000-7000-8000-00000000aaaa');
    expect(announced).not.toContain('<li');
  });

  it('says only what the request said: no deadline, no reference, no reasons, and no holes', () => {
    const bare = { ...sentForApproval, pending_approval: { status: 'pending_approval' } } as unknown as MigrationReply;
    const shown = textOf(render({ reply: bare, plan: asks }));
    expect(shown).toContain('Sent for approval');
    expect(shown).toContain('through the API');
    expect(shown).not.toContain('expires');
    expect(shown).not.toContain('reference');
    expect(shown).not.toContain('Why a second administrator is asked');
    expect(shown).not.toMatch(/[{}]|undefined/);
  });

  it('reads in Indonesian', () => {
    const indonesian = render({ reply: sentForApproval, plan: asks, catalogue: id });
    expect(textOf(indonesian)).toContain('Dikirim untuk persetujuan');
    expect(textOf(indonesian)).toContain('Diminta oleh Dita Larasati.');
    expect(textOf(indonesian)).toContain('Belum ada layar untuk ini: administrator menyetujui atau menolaknya melalui API.');
    expect(textOf(indonesian)).toMatch(/Referensi permintaan ini adalah\s+0199c0de-0000-7000-8000-00000000aaaa/);
    expect(textOf(indonesian)).toContain('Mengapa administrator kedua diminta');
    expect(buttons(indonesian)).toEqual(['Tutup']);
  });
});

describe('after an answer that could not be read', () => {
  const html = render({ reply: { applied: false, passed_over: [], passed_over_in_all: 0 } });
  const text = textOf(html);

  it('says so, and does not say that nothing was moved', () => {
    expect(text).toContain("The server's answer could not be read");
    expect(text).toContain('The migration may have been applied, or sent for approval: check the instances before trying again.');
    expect(text).not.toContain('Nothing was moved');
    expect(count(html, 'role="alert"')).toBe(1);
    expect(count(html, '<li')).toBe(0);
  });

  it('leaves the plan to be applied again once it has been looked at', () => {
    expect(buttons(html)).toEqual(['Close', 'Move 3 instances']);
    expect(hiddenButtons(html)).toEqual([]);
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

  it('holds the list in a named region that can scroll inside the dialog', () => {
    const region = /<div[^>]*data-scrollarea-viewport[^>]*>/.exec(html)?.[0] ?? '';
    expect(region).toContain('role="group"');
    const named = /aria-labelledby="([^"]+)"/.exec(region)?.[1] ?? 'no name';
    expect(html).toMatch(new RegExp(`<p[^>]*id="${named}"[^>]*>Instances that were not moved</p>`));
    expect(html).toContain('max-height');
  });

  it('is not a tab stop until it is known to scroll', () => {
    // Two lines that fit are not something to stop at on the way to Close.
    // Whether it scrolls is measured once it is laid out, which static markup
    // is not: the stop is added then (seen in a browser, not here).
    const region = /<div[^>]*data-scrollarea-viewport[^>]*>/.exec(html)?.[0] ?? '';
    expect(region).not.toContain('tabindex');
  });

  it('wraps a name with no break in it rather than cut it off', () => {
    const long = `Step${'x'.repeat(300)}`;
    const reply: MigrationReply = {
      plan: plan(),
      applied: true,
      passed_over_in_all: 1,
      passed_over: [{ ...left(1), cause: 'nowhere_to_land', steps: [{ node_id: long, name: long }] }],
    };
    const wide = render({ reply });
    const row = new RegExp(`<span[^>]*style="([^"]*)"[^>]*>[^<]*${long}`).exec(wide)?.[1] ?? '';
    expect(row).toContain('overflow-wrap:anywhere');
    // And the server's own sentences, which name steps too.
    const reasons = render({ reply: { ...sentForApproval, pending_approval: { ...sentForApproval.pending_approval, because: [long] } } as MigrationReply, plan: asks });
    expect(new RegExp(`<li[^>]*style="([^"]*)"[^>]*>(?:(?!</li>)[\\s\\S])*${long}`).exec(reasons)?.[1] ?? '').toContain('overflow-wrap:anywhere');
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
    expect(textOf(render({ reply: { ...passedOver, applied: false }, catalogue: id }))).toContain('Migrasi ini tidak memindahkan instansi mana pun');
    expect(indonesian).toContain('Diterapkan, tetapi tidak pada semua instansi');
    expect(indonesian).toContain('340 instansi tidak dipindahkan.');
    expect(indonesian).toContain('Instansi #000001');
    expect(indonesian).toContain('Instansi ini sudah tidak menunggu di "Operations approve"');
    expect(indonesian).toContain('dan 338 instansi lainnya');
  });
});
