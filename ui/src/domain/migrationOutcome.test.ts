import { describe, expect, it } from 'bun:test';

import { migrationRequestKey } from './instanceMigration';
import { versionPair } from './migrationDraft';
import {
  afterApply,
  answeredApply,
  failureToast,
  formatExpiry,
  MAX_PASSED_OVER_SHOWN,
  migrationNotice,
  migrationOutcome,
  outcomeNotice,
  outcomeOnScreen,
  passedOverCount,
  passedOverLine,
  saidInDialog,
} from './migrationOutcome';
import type { MigrationReply } from './migrationOutcome';
import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { format, type Values } from '../i18n/translate';
import type { ApiMigrationPlan, ApiPassedOver, ApiPendingApproval } from '../services/types';

const plan = (over: Partial<ApiMigrationPlan> = {}): ApiMigrationPlan => ({
  source_key: 'quotation',
  source_version: 2,
  target_version: 5,
  target_id: 'def-5',
  instances: 3,
  moves: [{ from: 'opsApprove', to: 'salesApprove', tokens: 3, tasks: 3, jobs: 0, mapped: true }],
  ...over,
});

describe('migrationNotice', () => {
  it('does not say anything moved when the server did not apply it', () => {
    // A reply with applied: false is a plan, whatever was asked for. Saying
    // "Moved" over it sends somebody away believing work changed version.
    const notice = migrationNotice({ plan: plan(), applied: false }, 5);
    expect(notice.title).toBe('Nothing was moved');
    expect(notice.message).toBe('The server worked out the plan but did not apply it, so every instance is where it was.');
    expect(notice.color).toBe('yellow');
    expect(notice.closes).toBe(false);
  });

  it('says so when nothing was left to move', () => {
    // Everything finished between the preview and the apply. The server
    // reports that as applied, over no instances at all.
    const notice = migrationNotice({ plan: plan({ instances: 0, moves: [] }), applied: true }, 5);
    expect(notice.title).toBe('Nothing to move');
    expect(notice.message).toBe('Nothing was running on v2 any more, so no instance changed version.');
  });

  it('says what moved, counting the reply rather than the preview', () => {
    const notice = migrationNotice({ plan: plan(), applied: true }, 5);
    expect(notice.title).toBe('Moved to v5');
    expect(notice.message).toBe('3 instances that were running on v2 now run on v5.');
    expect(migrationNotice({ plan: plan({ instances: 1 }), applied: true }, 5).message).toBe(
      '1 instance that was running on v2 now runs on v5.',
    );
  });

  it('does not claim that work it decided rather than moved runs on the new version', () => {
    // A cancelled instance ends where it is and keeps its version; a held one
    // is left on the old version for a person. Neither "now runs on v5".
    const decided = plan({
      actions: [
        { node_id: 'opsApprove', name: 'Operations approve', kind: 'cancel', reason: 'void' },
        { node_id: 'legalReview', kind: 'hold', reason: 'ask legal' },
        { node_id: 'creditCheck', name: 'Credit check', kind: 'skip', reason: 'waived' },
      ],
    });
    const notice = migrationNotice({ plan: decided, applied: true }, 5);
    expect(notice.title).toBe('Migration applied');
    expect(notice.message).toBe(
      'The 3 instances on v2 were dealt with. Those waiting at "Operations approve" were ended where they were, ' +
        'and keep v2. Those waiting at "legalReview" were left on v2 for somebody to decide. ' +
        'Those waiting at "Credit check" skipped it and carried on. ' +
        'Every other instance still running now runs on v5.',
    );
  });

  it('says whose work went back to the queue', () => {
    const holding = plan({
      moves: [{ from: 'opsApprove', to: 'salesApprove', tokens: 3, tasks: 3, tasks_claimed: 2, jobs: 0, mapped: true }],
    });
    expect(migrationNotice({ plan: holding, applied: true }, 5).message).toBe(
      '3 instances that were running on v2 now run on v5. 2 tasks somebody was holding went back to the queue.',
    );
  });
});

const inEnglish = (key: string, values?: Values) => format(en, key, values);
const inIndonesian = (key: string, values?: Values) => format(id, key, values);
const at = () => '6 Oct 2026, 09:12';

const pending: ApiPendingApproval = {
  request_id: '0199c0de-0000-7000-8000-00000000aaaa',
  status: 'pending_approval',
  requested_by: 'Dita Larasati',
  expires_at: '2026-10-06T09:12:00Z',
  because: [
    '“Operations approve” would be skipped for every listed instance waiting at it when the migration runs — not only those waiting there when this was asked for — and nobody would perform it',
  ],
};

/** As the server answers an apply it sent to somebody else: a 202's body. */
const sentForApproval: MigrationReply = {
  plan: plan({ requires_second_approver: true }),
  applied: false,
  passed_over: [],
  passed_over_in_all: 0,
  pending_approval: pending,
};

const left: ApiPassedOver = {
  instance_id: '0199c0de-0000-7000-8000-000000000001',
  cause: 'left_the_step',
  steps: [{ node_id: 'opsApprove', name: 'Operations approve' }],
  steps_in_all: 1,
  reason: 'It was no longer waiting at "Operations approve" when the migration reached it, so nothing was decided there and it was not moved.',
};
const other: ApiPassedOver = { ...left, instance_id: '0199c0de-0000-7000-8000-000000000002' };

describe('a migration sent for a second administrator', () => {
  it('says it was sent for approval, never that nothing moved', () => {
    // The server answers 202 with applied: false. "Nothing was moved — the
    // server did not apply it" would be false in the way that matters: a
    // request now waits on somebody else.
    const notice = outcomeNotice(sentForApproval, 5, inEnglish, at);
    expect(notice.title).toBe('Sent for approval');
    expect(notice.message).toBe(
      'Asked for by Dita Larasati. Nothing moves until it is approved. The administrator who asked cannot approve it, ' +
        'unless this organization has been set up as having one administrator. The request expires on 6 Oct 2026, 09:12.',
    );
    expect(notice.color).toBe('blue');
    expect(notice.title).not.toBe(migrationNotice(sentForApproval, 5).title);
  });

  it('does not say who else approves: who may is the organization’s to have set up', () => {
    // "A different administrator" was untrue of an organization set up as
    // having one administrator, where whoever asked approves their own request.
    for (const t of [inEnglish, inIndonesian]) {
      const outcome = migrationOutcome(sentForApproval, 5, t, at);
      expect(`${outcome.notice.message} ${outcome.toast.message}`).not.toMatch(/different administrator|administrator lain/);
    }
  });

  it('says how it is approved, and by what reference, since no screen shows it', () => {
    const outcome = migrationOutcome(sentForApproval, 5, inEnglish, at);
    expect(outcome.how).toBe('There is no screen for this yet: an administrator approves or rejects it through the API.');
    expect(outcome.reference).toEqual({
      before: "The request's reference is ",
      value: '0199c0de-0000-7000-8000-00000000aaaa',
      after: '.',
    });
    const indonesian = migrationOutcome(sentForApproval, 5, inIndonesian, at);
    expect(indonesian.how).toBe('Belum ada layar untuk ini: administrator menyetujui atau menolaknya melalui API.');
    expect(indonesian.reference).toEqual({ before: 'Referensi permintaan ini adalah ', value: '0199c0de-0000-7000-8000-00000000aaaa', after: '.' });
  });

  it('stays on screen: there is who asked, until when and why to read', () => {
    const outcome = migrationOutcome(sentForApproval, 5, inEnglish, at);
    expect(outcome.notice.closes).toBe(false);
    expect(outcome.waits).toBe(true);
    expect(saidInDialog(outcome)).toBe(true);
    expect(outcome.listTitle).toBe('Why an approval is asked');
    expect(outcome.reasons).toEqual(pending.because ?? []);
    expect(outcome.passedOver).toEqual([]);
    expect(outcome.more).toBeNull();
    expect(outcome.unread).toBe(false);
  });

  it('reads in Indonesian through the catalogue', () => {
    const outcome = migrationOutcome(sentForApproval, 5, inIndonesian, at);
    expect(outcome.notice.title).toBe('Dikirim untuk persetujuan');
    expect(outcome.notice.message).toBe(
      'Diminta oleh Dita Larasati. Tidak ada yang dipindahkan sampai ini disetujui. Administrator yang memintanya ' +
        'tidak dapat menyetujuinya, kecuali organisasi ini telah diatur memiliki satu administrator. ' +
        'Permintaan ini kedaluwarsa pada 6 Oct 2026, 09:12.',
    );
    expect(outcome.listTitle).toBe('Mengapa persetujuan diminta');
  });

  it('is worded from the request in the reply, whatever else the reply says', () => {
    // Not from a status, and not from the plan on screen: a plan that did not
    // say it would ask, and a reply that claims to have applied, still read as
    // a request that waits once the reply carries one.
    for (const reply of [
      { ...sentForApproval, plan: plan() },
      { ...sentForApproval, plan: undefined },
      { ...sentForApproval, applied: true },
      { ...sentForApproval, passed_over: [left], passed_over_in_all: 1 },
    ]) {
      const outcome = migrationOutcome(reply, 5, inEnglish, at);
      expect(outcome.notice.title).toBe('Sent for approval');
      expect(outcome.waits).toBe(true);
      expect(outcome.passedOver).toEqual([]);
    }
  });

  it('says nothing of who asked when the reply does not name them', () => {
    const unnamed = { ...sentForApproval, pending_approval: { ...pending, requested_by: ' ', because: undefined } };
    const outcome = migrationOutcome(unnamed, 5, inEnglish, at);
    expect(outcome.notice.message).toBe(
      'Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization ' +
        'has been set up as having one administrator. The request expires on 6 Oct 2026, 09:12.',
    );
    expect(outcome.reasons).toEqual([]);
    expect(saidInDialog(outcome)).toBe(true);
  });

  it('leaves out whatever a malformed request does not say, and never throws or shows a placeholder', () => {
    // A reply this bundle cannot read whole is still a request that was sent.
    // An exception here would be the dialog's "the server did not confirm the
    // move", or a blank screen; "{date}" would be a sentence with a hole in it.
    const rule =
      'Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization ' +
      'has been set up as having one administrator.';
    const malformed: unknown[] = [
      {},
      { request_id: 7, status: null, requested_by: { name: 'Dita' }, expires_at: 1791278000, because: 'because' },
      { requested_by: null, expires_at: '', because: [null, 4, ' ', { why: 'x' }] },
      { expires_at: 'soon' },
      true,
      'pending',
    ];
    for (const pendingApproval of malformed) {
      const reply = { ...sentForApproval, pending_approval: pendingApproval } as unknown as MigrationReply;
      const real = (iso: string) => formatExpiry(iso, 'en', 'UTC');
      for (const t of [inEnglish, inIndonesian]) {
        const outcome = migrationOutcome(reply, 5, t, real);
        expect(outcome.waits).toBe(true);
        expect(outcome.reasons).toEqual([]);
        expect(outcome.reference).toBeNull();
        expect(`${outcome.notice.message} ${outcome.toast.message}`).not.toMatch(/[{}]|undefined|null|NaN|Invalid|object/);
      }
      const outcome = migrationOutcome(reply, 5, inEnglish, real);
      expect(outcome.notice.title).toBe('Sent for approval');
      expect(outcome.notice.message).toBe(rule);
      expect(outcome.toast.message).toBe(`${rule} There is no screen for this yet: an administrator approves or rejects it through the API.`);
    }
  });

  it('formats the deadline in the reader’s language, and names the time zone it is in', () => {
    // "4:12 PM" is a different moment for the administrator who asked and the
    // one who approves, when they are not in the same place.
    //
    // The parts are asserted, not one formatter's whole string: the words a
    // runtime's locale data uses for a month, a separator or a zone differ
    // between versions of that data, and this test is about what the date
    // has to carry — the day, the year, the hour in the zone asked for, and
    // a name for that zone — not about how one build spells it.
    const parts = (text: string) => ({
      year: /\b2026\b/.test(text),
      day: /\b6\b/.test(text),
      // A zone's own name at the end — "UTC", "GMT+7", "WIB" — and not any
      // run of capitals: "AM" and "PM" end an English time and are no zone.
      zone: /(UTC|GMT[+-]\d{1,2}(:\d{2})?|WIB)$/.test(text),
    });
    for (const locale of ['en', 'id']) {
      const utc = formatExpiry('2026-10-06T09:12:00Z', locale, 'UTC');
      expect(parts(utc), utc).toEqual({ year: true, day: true, zone: true });
      // Twelve past nine in the morning, however the hour and minute are joined.
      expect(utc, utc).toMatch(/\b0?9[:.]12\b/);
      expect(utc, utc).toMatch(/UTC$/);
      const jakarta = formatExpiry('2026-10-06T09:12:00Z', locale, 'Asia/Jakarta');
      expect(parts(jakarta), jakarta).toEqual({ year: true, day: true, zone: true });
      // Seven hours on: twelve past four in the afternoon, on a twelve- or a
      // twenty-four-hour clock, and not said to be UTC.
      expect(jakarta, jakarta).toMatch(/\b(4|16)[:.]12\b/);
      expect(jakarta, jakarta).not.toMatch(/UTC$/);
      expect(jakarta).not.toBe(utc);
    }
    // And the two languages do not say it the same way.
    expect(formatExpiry('2026-10-06T09:12:00Z', 'id', 'UTC')).not.toBe(formatExpiry('2026-10-06T09:12:00Z', 'en', 'UTC'));
  });

  it('does not fail on a deadline or a language it cannot read', () => {
    // An exception here would land in the dialog's "the server did not confirm
    // the move" — an error over a request that was in fact sent. A deadline
    // that cannot be read is left out, not printed as it came.
    expect(formatExpiry('soon', 'en')).toBe('');
    expect(formatExpiry('', 'en')).toBe('');
    expect(formatExpiry(undefined as unknown as string, 'en')).toBe('');
    expect(formatExpiry('2026-10-06T09:12:00Z', 'not a language', 'UTC')).toMatch(/2026/);
  });
});

describe('an apply that passed instances over', () => {
  it('does not say every instance moved when some were not', () => {
    // The plan counted three; the run moved two. "3 instances … now run on v5"
    // is what the dialog said, read from the plan, whatever the run had done.
    const reply = { plan: plan(), applied: true, passed_over: [left], passed_over_in_all: 1 };
    const notice = outcomeNotice(reply, 5, inEnglish, at);
    expect(notice.title).toBe('Applied, but not to every instance');
    expect(notice.message).toBe('1 instance was not moved. The list below says why.');
    expect(notice.color).toBe('yellow');
    expect(notice.closes).toBe(false);
    expect(notice.message).not.toBe(migrationNotice(reply, 5).message);
  });

  it('says no instance was moved when the run passed every one over', () => {
    const reply = { plan: plan(), applied: false, passed_over: [left, other], passed_over_in_all: 2 };
    const notice = outcomeNotice(reply, 5, inEnglish, at);
    // Not "No instance was moved": one in the list may have been moved by
    // another run (already_moved). What this run did is what it can say.
    expect(notice.title).toBe('This run moved no instance');
    expect(notice.message).toBe('2 instances were not moved. The list below says why.');
    expect(notice.color).toBe('yellow');
    expect(notice.closes).toBe(false);
    const indonesian = outcomeNotice(reply, 5, inIndonesian, at);
    expect(indonesian.title).toBe('Migrasi ini tidak memindahkan instansi mana pun');
    expect(indonesian.message).toBe('2 instansi tidak dipindahkan. Daftar di bawah menjelaskan alasannya.');
  });

  it('counts what the server counted, not what it listed', () => {
    // The reply lists two hundred at most and says how many there were.
    const reply = { plan: plan(), applied: true, passed_over: [left, other], passed_over_in_all: 340 };
    expect(passedOverCount(reply)).toBe(340);
    expect(outcomeNotice(reply, 5, inEnglish, at).message).toBe('340 instances were not moved. The list below says why.');
    const outcome = migrationOutcome(reply, 5, inEnglish, at);
    expect(outcome.passedOver).toHaveLength(2);
    expect(outcome.more).toBe('and 338 more instances');
    expect(migrationOutcome(reply, 5, inIndonesian, at).more).toBe('dan 338 instansi lainnya');
    expect(migrationOutcome({ ...reply, passed_over_in_all: 3 }, 5, inEnglish, at).more).toBe('and 1 more instance');
  });

  it('counts the list for a server that sends no count', () => {
    const reply = { plan: plan(), applied: true, passed_over: [left, other] };
    expect(passedOverCount(reply)).toBe(2);
    expect(migrationOutcome(reply, 5, inEnglish, at).more).toBeNull();
    expect(passedOverCount({ plan: plan(), applied: true })).toBe(0);
    // A count below the list is not believed over the list itself.
    expect(passedOverCount({ ...reply, passed_over_in_all: 1 })).toBe(2);
  });

  it('never lists more than the dialog is prepared to draw', () => {
    const many = Array.from({ length: MAX_PASSED_OVER_SHOWN + 25 }, (_, n) => ({
      ...left,
      instance_id: `0199c0de-0000-7000-8000-${String(n).padStart(12, '0')}`,
    }));
    const outcome = migrationOutcome({ plan: plan(), applied: true, passed_over: many, passed_over_in_all: 900 }, 5, inEnglish, at);
    expect(outcome.passedOver).toHaveLength(MAX_PASSED_OVER_SHOWN);
    expect(outcome.more).toBe(`and ${900 - MAX_PASSED_OVER_SHOWN} more instances`);
  });

  it('names each instance as the instance list does, never by its whole id', () => {
    const outcome = migrationOutcome({ plan: plan(), applied: true, passed_over: [left, other], passed_over_in_all: 2 }, 5, inEnglish, at);
    expect(outcome.listTitle).toBe('Instances that were not moved');
    expect(outcome.passedOver.map((row) => row.instance)).toEqual(['Instance #000001', 'Instance #000002']);
    expect(outcome.passedOver[0].why).toBe(passedOverLine(left, 2, inEnglish));
    expect(new Set(outcome.passedOver.map((row) => row.key)).size).toBe(2);
    expect(outcome.passedOver.map((row) => `${row.instance} ${row.why}`).join(' ')).not.toContain(left.instance_id);
    expect(migrationOutcome({ plan: plan(), applied: true, passed_over: [left] }, 5, inIndonesian, at).passedOver[0].instance).toBe(
      'Instansi #000001',
    );
  });

  it('says why in the reader’s language, naming the step as people know it', () => {
    expect(passedOverLine(left, 2, inEnglish)).toBe(
      'It was no longer waiting at "Operations approve" when the migration reached it, so nothing was decided there. ' +
        'It stays on v2; if it is still running, apply the same migration again to plan for where it is now.',
    );
    expect(passedOverLine(left, 2, inIndonesian)).toBe(
      'Instansi ini sudah tidak menunggu di "Operations approve" saat migrasi sampai padanya, jadi tidak ada yang ' +
        'diputuskan di sana. Instansi tetap di v2; jika masih berjalan, terapkan migrasi yang sama lagi untuk ' +
        'merencanakan dari posisinya sekarang.',
    );
    const two: ApiPassedOver = {
      ...left,
      cause: 'nowhere_to_land',
      steps: [{ node_id: 'legalReview', name: 'Legal review' }, { node_id: 'creditCheck', name: 'Credit check' }],
      steps_in_all: 2,
    };
    const line = passedOverLine(two, 2, inEnglish);
    expect(line).toContain('"Legal review", "Credit check"');
    expect(line).not.toContain('legalReview');
  });

  it('says how many more steps there were than the server listed', () => {
    const cut: ApiPassedOver = {
      ...left,
      cause: 'nowhere_to_land',
      steps: [{ node_id: 'legalReview', name: 'Legal review' }, { node_id: 'creditCheck', name: 'Credit check' }],
      steps_in_all: 17,
    };
    expect(passedOverLine(cut, 2, inEnglish)).toBe(
      'It had work at "Legal review", "Credit check" and 15 more, which the new version has nowhere to put. ' +
        'It stays on v2; plan again with a mapping or a decision for that work.',
    );
    expect(passedOverLine(cut, 2, inIndonesian)).toContain('"Legal review", "Credit check" dan 15 lainnya,');
  });

  it('says a step’s name as it is written, whatever is in it', () => {
    // A name is the modeller's, and reaches the sentence as a value: braces
    // are not placeholders, "#" is not a count, and "$&" is not a pattern.
    const odd = 'Check {version} {steps} #1 — O\'Brien\'s $& $1 $$ {count, plural, other {#}}';
    const named: ApiPassedOver = { ...left, cause: 'nowhere_to_land', steps: [{ node_id: 'odd', name: odd }], steps_in_all: 1 };
    expect(passedOverLine(named, 2, inEnglish)).toBe(
      `It had work at "${odd}", which the new version has nowhere to put. It stays on v2; plan again with a mapping or a decision for that work.`,
    );
    expect(passedOverLine(named, 2, inIndonesian)).toBe(
      `Instansi ini punya pekerjaan di "${odd}", yang tidak punya tempat di versi baru. Instansi tetap di v2; rencanakan lagi dengan pemetaan atau keputusan untuk pekerjaan itu.`,
    );
    const cut = { ...named, steps_in_all: 3 };
    expect(passedOverLine(cut, 2, inEnglish)).toContain(`"${odd}" and 2 more,`);
    expect(passedOverLine(cut, 2, inIndonesian)).toContain(`"${odd}" dan 2 lainnya,`);
  });

  it('does not throw on an entry that is not as the server writes it', () => {
    const broken = [
      { instance_id: 9, cause: 'nowhere_to_land', steps: 'opsApprove', steps_in_all: 'many', reason: null },
      { cause: 'left_the_step', steps: [null, { name: 4 }, { node_id: 'opsApprove' }], reason: 'The server said why.' },
      null,
    ] as unknown as ApiPassedOver[];
    const outcome = migrationOutcome({ plan: plan(), applied: true, passed_over: broken, passed_over_in_all: 'three' as unknown as number }, 5, inEnglish, at);
    expect(outcome.passedOver).toHaveLength(3);
    expect(outcome.notice.message).toBe('3 instances were not moved. The list below says why.');
    for (const row of outcome.passedOver) expect(`${row.instance} ${row.why}`).not.toMatch(/[{}]|undefined|null|NaN|object/);
    expect(outcome.passedOver[1].why).toContain('"opsApprove"');
  });

  it('has words for every cause the server sends, in both languages', () => {
    const causes = ['already_moved', 'counters_would_merge', 'left_the_step', 'left_where_nothing_decides',
      'no_longer_running', 'not_planned_for', 'nowhere_to_land', 'waiting_to_be_decided'];
    const steps = [{ node_id: 'opsApprove', name: 'Operations approve' }];
    for (const cause of causes) {
      for (const t of [inEnglish, inIndonesian]) {
        const line = passedOverLine({ instance_id: 'i-1', cause, steps, steps_in_all: 1, reason: 'The server’s sentence.' }, 2, t);
        expect(line).not.toBe('The server’s sentence.');
        expect(line).not.toMatch(/[{}]/);
        expect(line).not.toContain('migration.passedOver');
      }
    }
  });

  it('falls back to the server’s sentence for a cause it has no words for', () => {
    // A server newer than this bundle may send a cause the catalogue lacks, and
    // an older one sends none. The bare key on screen would say nothing; the
    // server's own sentence does.
    expect(passedOverLine({ instance_id: 'i-3', cause: 'something_new', reason: 'The server said why.' }, 2, inEnglish)).toBe(
      'The server said why.',
    );
    expect(passedOverLine({ instance_id: 'i-4', reason: 'An older server sent no cause.' }, 2, inIndonesian)).toBe(
      'An older server sent no cause.',
    );
  });

  it('falls back to the server’s sentence rather than leave a hole in its own', () => {
    // "It had work at , which the new version…" and "It stays on vundefined"
    // are worse than the server's English: a sentence with a hole in it.
    const noSteps: ApiPassedOver = { instance_id: 'i-5', cause: 'nowhere_to_land', steps: [], steps_in_all: 0, reason: 'The server named them.' };
    expect(passedOverLine(noSteps, 2, inEnglish)).toBe('The server named them.');
    expect(passedOverLine({ ...noSteps, steps: undefined }, 2, inIndonesian)).toBe('The server named them.');
    expect(passedOverLine(left, undefined, inEnglish)).toBe(left.reason);
    // A cause that is about no step needs none.
    expect(passedOverLine({ instance_id: 'i-6', cause: 'already_moved', steps: [], steps_in_all: 0, reason: 'x' }, 2, inEnglish)).toBe(
      'Another run of a migration had already moved it, so it was not moved again.',
    );
  });
});

describe('what an apply did, case by case', () => {
  const some = [left];
  const cases: { name: string; reply: MigrationReply; title: string; color: string; closes: boolean; inDialog: boolean }[] = [
    { name: 'moved every instance', reply: { plan: plan(), applied: true, passed_over: [], passed_over_in_all: 0 },
      title: 'Moved to v5', color: 'green', closes: true, inDialog: false },
    { name: 'moved some and passed some over', reply: { plan: plan(), applied: true, passed_over: some, passed_over_in_all: 1 },
      title: 'Applied, but not to every instance', color: 'yellow', closes: false, inDialog: true },
    { name: 'made a skip and then moved nobody', reply: { plan: plan({ instances: 1 }), applied: true, passed_over: some, passed_over_in_all: 1 },
      title: 'Applied, but not to every instance', color: 'yellow', closes: false, inDialog: true },
    { name: 'passed everybody over', reply: { plan: plan(), applied: false, passed_over: some, passed_over_in_all: 1 },
      title: 'This run moved no instance', color: 'yellow', closes: false, inDialog: true },
    { name: 'found nothing to do', reply: { plan: plan({ instances: 0, moves: [] }), applied: true, passed_over: [], passed_over_in_all: 0 },
      title: 'Nothing to move', color: 'gray', closes: true, inDialog: false },
    { name: 'found only instances that arrived since the plan', reply: { plan: plan({ instances: 0, moves: [] }), applied: false, passed_over: some, passed_over_in_all: 1 },
      title: 'This run moved no instance', color: 'yellow', closes: false, inDialog: true },
    { name: 'was sent for approval', reply: sentForApproval,
      title: 'Sent for approval', color: 'blue', closes: false, inDialog: true },
    { name: 'answered a plan and applied nothing', reply: { plan: plan(), applied: false, passed_over: [], passed_over_in_all: 0 },
      title: 'Nothing was moved', color: 'yellow', closes: false, inDialog: false },
    { name: 'was answered with nothing that could be read', reply: { applied: false, passed_over: [], passed_over_in_all: 0 },
      title: 'The server\'s answer could not be read', color: 'yellow', closes: false, inDialog: true },
  ];

  for (const c of cases) {
    it(`an apply that ${c.name}`, () => {
      const outcome = migrationOutcome(c.reply, 5, inEnglish, at);
      expect(outcome.notice.title).toBe(c.title);
      expect(outcome.notice.color).toBe(c.color as typeof outcome.notice.color);
      expect(outcome.notice.closes).toBe(c.closes);
      expect(saidInDialog(outcome)).toBe(c.inDialog);
    });
  }

  it('is kept on screen or said in a toast, never both and never neither, and the dialog closes only on a toast', () => {
    for (const c of cases) {
      const outcome = migrationOutcome(c.reply, 5, inEnglish, at);
      const next = afterApply(outcome);
      expect(next.keep, c.name).toBe(c.inDialog);
      expect(next.toast === null, c.name).toBe(c.inDialog);
      expect(next.close, c.name).toBe(!c.inDialog && c.closes);
      expect(next.keep && next.close, c.name).toBe(false);
    }
  });

  it('says it in a toast after all when the dialog was closed before the answer came', () => {
    // Nothing is left to keep it in. Said nowhere, a request that was sent
    // would be a press that did nothing anybody could see. And a dialog that
    // is closed is not closed again: it may have been opened since, for
    // another version.
    for (const c of cases) {
      const next = afterApply(migrationOutcome(c.reply, 5, inEnglish, at), true, false);
      expect(next.keep, c.name).toBe(false);
      // A request that was sent names its two versions in the toast: there
      // is no dialog left to say which migration it was.
      expect(next.toast?.title, c.name).toBe(c.reply.pending_approval ? `${c.title}: v2 → v5` : c.title);
      expect(next.replan, c.name).toBe(false);
      expect(next.close, c.name).toBe(false);
    }
  });

  it('says it in a toast when the dialog is open and will not show it', () => {
    // The form was edited while the apply was on its way. The answer is to a
    // request that is no longer the one on screen, so the screen will not show
    // it — and a request now waits on the server. It is said, in a toast, as
    // for a dialog that was closed.
    const next = afterApply(migrationOutcome(sentForApproval, 5, inEnglish, at), false, true);
    expect(next.keep).toBe(false);
    // And it says which migration: the dialog under it shows another now.
    expect(next.toast?.title).toBe('Sent for approval: v2 → v5');
    expect(next.replan).toBe(false);
    expect(next.close).toBe(false);
    // Every case: what the screen will not show is a toast, whatever it is.
    for (const c of cases) {
      const late = afterApply(migrationOutcome(c.reply, 5, inEnglish, at), false, true);
      expect(late.keep, c.name).toBe(false);
      expect(late.toast, c.name).not.toBeNull();
    }
  });

  it('plans again after a run that left instances behind, and not after a request that changed nothing', () => {
    // The plan in hand counts instances the run has since dealt with; what is
    // left is what the next apply is for. A request that waits moved nothing,
    // and the plan on screen is the one that was sent.
    const plansAgain = (reply: MigrationReply) => afterApply(migrationOutcome(reply, 5, inEnglish, at)).replan;
    expect(plansAgain({ plan: plan(), applied: true, passed_over: some, passed_over_in_all: 1 })).toBe(true);
    expect(plansAgain({ plan: plan(), applied: false, passed_over: some, passed_over_in_all: 1 })).toBe(true);
    expect(plansAgain(sentForApproval)).toBe(false);
    // An answer nobody could read: whatever it did, the plan in hand may be old.
    expect(plansAgain({ applied: false })).toBe(true);
    // As before: a toast, and the dialog as it was.
    expect(plansAgain({ plan: plan(), applied: true })).toBe(false);
    expect(plansAgain({ plan: plan(), applied: false })).toBe(false);
  });

  it('leaves every reply that waits on nobody and passed nobody over to migrationNotice', () => {
    for (const reply of [
      { plan: plan(), applied: true },
      { plan: plan(), applied: false },
      { plan: plan(), applied: true, passed_over: [], passed_over_in_all: 0 },
      { plan: plan({ instances: 0, moves: [] }), applied: true, passed_over: [] },
    ]) {
      expect(outcomeNotice(reply, 5, inEnglish, at)).toEqual(migrationNotice(reply, 5));
      expect(outcomeNotice(reply, 5, inIndonesian, at)).toEqual(migrationNotice(reply, 5));
    }
  });
});

describe('what a toast says of an answer the dialog cannot keep', () => {
  const rule =
    'Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization ' +
    'has been set up as having one administrator.';

  it('says a request was sent whole: who, the rule, until when, how, and its reference — and stays until dismissed', () => {
    const toast = migrationOutcome(sentForApproval, 5, inEnglish, at).toast;
    // The dialog it was sent from may show another plan by the time it is
    // read, with its own button: the toast says which migration was sent.
    expect(toast.title).toBe('Sent for approval: v2 → v5');
    expect(migrationOutcome(sentForApproval, 5, inIndonesian, at).toast.title).toBe('Dikirim untuk persetujuan: v2 → v5');
    // In the dialog, under the versions it shows, the title is as it was.
    expect(migrationOutcome(sentForApproval, 5, inEnglish, at).notice.title).toBe('Sent for approval');
    expect(toast.message).toBe(
      `Asked for by Dita Larasati. ${rule} The request expires on 6 Oct 2026, 09:12. ` +
        'There is no screen for this yet: an administrator approves or rejects it through the API. ' +
        "The request's reference is 0199c0de-0000-7000-8000-00000000aaaa.",
    );
    expect(toast.color).toBe('blue');
    expect(toast.closes).toBe(false);
    // It is the only place the request's reference is left once the dialog
    // has gone, and four seconds is not long enough to copy it.
    expect(toast.stays).toBe(true);
    expect(migrationOutcome(sentForApproval, 5, inIndonesian, at).toast.message).toBe(
      'Diminta oleh Dita Larasati. Tidak ada yang dipindahkan sampai ini disetujui. Administrator yang memintanya ' +
        'tidak dapat menyetujuinya, kecuali organisasi ini telah diatur memiliki satu administrator. ' +
        'Permintaan ini kedaluwarsa pada 6 Oct 2026, 09:12. ' +
        'Belum ada layar untuk ini: administrator menyetujui atau menolaknya melalui API. ' +
        'Referensi permintaan ini adalah 0199c0de-0000-7000-8000-00000000aaaa.',
    );
  });

  it('never points at a list that is not there', () => {
    // The dialog's own sentence ends "The list below says why." A toast has
    // nothing below it. It says how many, which is all it can say truly: a
    // plan made again counts what is on the old version, not which instances
    // this run passed over or why.
    const some = migrationOutcome({ plan: plan(), applied: true, passed_over: [left], passed_over_in_all: 1 }, 5, inEnglish, at);
    expect(some.notice.message).toBe('1 instance was not moved. The list below says why.');
    expect(some.toast).toEqual({ title: 'Applied, but not to every instance', message: '1 instance was not moved.', color: 'yellow', closes: false });
    const all = { plan: plan(), applied: false, passed_over: [left, other], passed_over_in_all: 340 };
    expect(migrationOutcome(all, 5, inEnglish, at).toast).toEqual({
      title: 'This run moved no instance',
      message: '340 instances were not moved.',
      color: 'yellow',
      closes: false,
    });
    expect(migrationOutcome(all, 5, inIndonesian, at).toast).toEqual({
      title: 'Migrasi ini tidak memindahkan instansi mana pun',
      message: '340 instansi tidak dipindahkan.',
      color: 'yellow',
      closes: false,
    });
    for (const reply of [sentForApproval, all, { plan: plan(), applied: true, passed_over: [left] }, { applied: false }]) {
      for (const t of [inEnglish, inIndonesian]) {
        expect(migrationOutcome(reply, 5, t, at).toast.message).not.toMatch(/below|di bawah/);
      }
    }
  });

  it('says an answer could not be read, and what that leaves unknown', () => {
    // A 200 or a 202 whose body is not what the route writes. "Nothing was
    // moved" is the one thing it cannot be taken to mean.
    for (const reply of [{ applied: false }, { applied: true }, { applied: false, passed_over: [], passed_over_in_all: 0 }]) {
      const outcome = migrationOutcome(reply, 5, inEnglish, at);
      expect(outcome.unread).toBe(true);
      expect(outcome.notice).toEqual({
        title: "The server's answer could not be read",
        message:
          'The server answered, but its answer could not be read. The migration may have been applied, or sent for approval: ' +
          'check the instances before trying again. A request that was sent is among those waiting for approval ' +
          '(GET /api/v1/deviation-requests), and sending the same migration again answers with that request and makes no second one.',
        color: 'yellow',
        closes: false,
        stays: true,
      });
      expect(outcome.toast).toEqual(outcome.notice);
      expect(outcome.notice.title).not.toBe(migrationNotice(reply, 5).title);
    }
    const indonesian = migrationOutcome({ applied: false }, 5, inIndonesian, at).notice;
    expect(indonesian.title).toBe('Jawaban server tidak dapat dibaca');
    expect(indonesian.message).toBe(
      'Server menjawab, tetapi jawabannya tidak dapat dibaca. Migrasi mungkin sudah diterapkan, atau dikirim untuk persetujuan: ' +
        'periksa instansinya sebelum mencoba lagi. Permintaan yang sudah terkirim ada di antara yang menunggu persetujuan ' +
        '(GET /api/v1/deviation-requests), dan mengirim ulang migrasi yang sama dijawab dengan permintaan itu, tanpa membuat permintaan kedua.',
    );
  });

  it('is the notice itself for every answer that was always a toast', () => {
    for (const reply of [{ plan: plan(), applied: true }, { plan: plan(), applied: false }, { plan: plan({ instances: 0, moves: [] }), applied: true }]) {
      const outcome = migrationOutcome(reply, 5, inEnglish, at);
      expect(outcome.toast).toEqual(migrationNotice(reply, 5));
      expect(outcome.notice).toEqual(migrationNotice(reply, 5));
    }
  });

  it('says a refusal that arrived after the dialog had closed, in the server’s words', () => {
    expect(failureToast('only an administrator may migrate running instances', inEnglish)).toEqual({
      title: 'The migration ended with an error',
      message: 'only an administrator may migrate running instances',
      color: 'red',
      closes: false,
      stays: true,
    });
    expect(failureToast('x', inIndonesian).title).toBe('Migrasi berakhir dengan kesalahan');
  });
});

describe('what stays on screen after an apply', () => {
  const answered = (reply: MigrationReply) => ({ pair: 'def-2→def-5', requestKey: 'the request', reply, target: 5 });
  const passedOver: MigrationReply = { plan: plan(), applied: true, passed_over: [left], passed_over_in_all: 1 };

  it('is nothing until an apply has answered, and nothing for an answer a toast said whole', () => {
    expect(outcomeOnScreen(null, 'def-2→def-5', 'the request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered({ plan: plan(), applied: true }), 'def-2→def-5', 'the request', inEnglish, at)).toBeNull();
  });

  it('keeps a request that waits for as long as it is the request on screen', () => {
    // Edit the mapping and the screen describes another migration: "sent for
    // approval" would be said of something nobody has sent.
    const shown = outcomeOnScreen(answered(sentForApproval), 'def-2→def-5', 'the request', inEnglish, at);
    expect(shown?.waits).toBe(true);
    expect(outcomeOnScreen(answered(sentForApproval), 'def-2→def-5', 'an edited request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered(sentForApproval), 'def-2→def-5', null, inEnglish, at)).toBeNull();
  });

  it('keeps the instances passed over while the request is edited: they are what it is edited for', () => {
    const shown = outcomeOnScreen(answered(passedOver), 'def-2→def-5', 'an edited request', inEnglish, at);
    expect(shown?.passedOver).toHaveLength(1);
    expect(shown?.waits).toBe(false);
  });

  it('keeps an answer nobody could read, for the two versions it was about', () => {
    const unread = outcomeOnScreen(answered({ applied: false }), 'def-2→def-5', 'an edited request', inEnglish, at);
    expect(unread?.unread).toBe(true);
    expect(outcomeOnScreen(answered({ applied: false }), 'def-3→def-5', 'the request', inEnglish, at)).toBeNull();
  });

  it('never shows one pair of versions what an apply did to another', () => {
    expect(outcomeOnScreen(answered(passedOver), 'def-3→def-5', 'the request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered(sentForApproval), 'def-3→def-5', 'the request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered(passedOver), null, null, inEnglish, at)).toBeNull();
  });
});

describe('the last apply, from what was sent and what came back', () => {
  const sent = { source: 'def-2', target: 'def-5', mapping: { opsApprove: 'salesApprove' } };
  const reply: MigrationReply = { plan: plan(), applied: true, passed_over: [left], passed_over_in_all: 1 };

  it('is nothing until something was sent and answered', () => {
    expect(answeredApply(undefined, undefined, 5)).toBeNull();
    // On its way, or thrown: what was sent, and no answer.
    expect(answeredApply(sent, undefined, 5)).toBeNull();
    expect(answeredApply(undefined, reply, 5)).toBeNull();
  });

  it('is nothing for a refusal the reply carries: that is an error, shown as one', () => {
    expect(answeredApply(sent, { ...reply, err: 'invalid argument: the plan no longer holds' }, 5)).toBeNull();
  });

  it('is kept with the pair and the request it answered, named as the dialog names them', () => {
    const answered = answeredApply(sent, reply, 5);
    expect(answered?.pair).toBe(versionPair('def-2', 'def-5'));
    expect(answered?.requestKey).toBe(migrationRequestKey({ ...sent, acknowledge: [], actions: {} }));
    expect(answered?.reply).toBe(reply);
    const decided = { ...sent, acknowledge: ['legalReview'], actions: { creditCheck: { kind: 'skip' as const, reason: 'waived' } } };
    expect(answeredApply(decided, reply, 5)?.requestKey).toBe(migrationRequestKey(decided));
    expect(answeredApply(decided, reply, 5)?.requestKey).not.toBe(answered?.requestKey);
  });

  it('names the version the server says it moved to, and the one on screen when it says none', () => {
    expect(answeredApply(sent, { ...reply, plan: plan({ target_version: 6 }) }, 5)?.target).toBe(6);
    expect(answeredApply(sent, { ...reply, plan: undefined }, 5)?.target).toBe(5);
  });
});
