import { describe, expect, it } from 'bun:test';

import {
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
      'Asked for by Dita Larasati. Nothing moves until a different administrator approves it. ' +
        'The request expires on 6 Oct 2026, 09:12.',
    );
    expect(notice.color).toBe('blue');
    expect(notice.title).not.toBe(migrationNotice(sentForApproval, 5).title);
  });

  it('stays on screen: there is who asked, until when and why to read', () => {
    const outcome = migrationOutcome(sentForApproval, 5, inEnglish, at);
    expect(outcome.notice.closes).toBe(false);
    expect(outcome.waits).toBe(true);
    expect(saidInDialog(outcome)).toBe(true);
    expect(outcome.listTitle).toBe('Why a second administrator is asked');
    expect(outcome.reasons).toEqual(pending.because ?? []);
    expect(outcome.passedOver).toEqual([]);
    expect(outcome.more).toBeNull();
  });

  it('reads in Indonesian through the catalogue', () => {
    const outcome = migrationOutcome(sentForApproval, 5, inIndonesian, at);
    expect(outcome.notice.title).toBe('Dikirim untuk persetujuan');
    expect(outcome.notice.message).toBe(
      'Diminta oleh Dita Larasati. Tidak ada yang dipindahkan sampai administrator lain menyetujuinya. ' +
        'Permintaan ini kedaluwarsa pada 6 Oct 2026, 09:12.',
    );
    expect(outcome.listTitle).toBe('Mengapa administrator kedua diminta');
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
      'Nothing moves until a different administrator approves it. The request expires on 6 Oct 2026, 09:12.',
    );
    expect(outcome.reasons).toEqual([]);
    expect(saidInDialog(outcome)).toBe(true);
  });

  it('formats the deadline in the reader’s language', () => {
    const english = formatExpiry('2026-10-06T09:12:00Z', 'en', 'UTC');
    const indonesian = formatExpiry('2026-10-06T09:12:00Z', 'id', 'UTC');
    expect(english).toMatch(/Oct 6, 2026/);
    expect(english).toMatch(/9:12/);
    expect(indonesian).toMatch(/6 Okt 2026/);
    expect(indonesian).toMatch(/09[.:]12/);
  });

  it('does not fail on a deadline or a language it cannot read', () => {
    // An exception here would land in the dialog's "the server did not confirm
    // the move" — an error over a request that was in fact sent.
    expect(formatExpiry('soon', 'en')).toBe('soon');
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
    expect(notice.title).toBe('No instance was moved');
    expect(notice.message).toBe('2 instances were not moved. The list below says why.');
    expect(notice.color).toBe('yellow');
    expect(notice.closes).toBe(false);
    const indonesian = outcomeNotice(reply, 5, inIndonesian, at);
    expect(indonesian.title).toBe('Tidak ada instansi yang dipindahkan');
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
      title: 'No instance was moved', color: 'yellow', closes: false, inDialog: true },
    { name: 'found nothing to do', reply: { plan: plan({ instances: 0, moves: [] }), applied: true, passed_over: [], passed_over_in_all: 0 },
      title: 'Nothing to move', color: 'gray', closes: true, inDialog: false },
    { name: 'found only instances that arrived since the plan', reply: { plan: plan({ instances: 0, moves: [] }), applied: false, passed_over: some, passed_over_in_all: 1 },
      title: 'No instance was moved', color: 'yellow', closes: false, inDialog: true },
    { name: 'was sent for approval', reply: sentForApproval,
      title: 'Sent for approval', color: 'blue', closes: false, inDialog: true },
    { name: 'answered a plan and applied nothing', reply: { plan: plan(), applied: false, passed_over: [], passed_over_in_all: 0 },
      title: 'Nothing was moved', color: 'yellow', closes: false, inDialog: false },
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

  it('never shows one pair of versions what an apply did to another', () => {
    expect(outcomeOnScreen(answered(passedOver), 'def-3→def-5', 'the request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered(sentForApproval), 'def-3→def-5', 'the request', inEnglish, at)).toBeNull();
    expect(outcomeOnScreen(answered(passedOver), null, null, inEnglish, at)).toBeNull();
  });
});
