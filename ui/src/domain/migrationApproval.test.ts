import { describe, expect, it } from 'bun:test';

import { applyLabel, approvalNeeded } from './migrationApproval';
import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { format, type Values } from '../i18n/translate';
import type { ApiMigrationPlan } from '../services/types';

const inEnglish = (key: string, values?: Values) => format(en, key, values);
const inIndonesian = (key: string, values?: Values) => format(id, key, values);

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

const reasons = [
  '“Operations approve” would be skipped for every listed instance waiting at it when the migration runs — not only those waiting there when this was asked for — and nobody would perform it',
  'and 2 more reasons',
];

describe('a plan that needs a second administrator', () => {
  it('says so before anything is sent, with the server’s reasons as it gave them', () => {
    const needed = approvalNeeded(plan({ requires_second_approver: true, second_approver_reasons: reasons }), inEnglish);
    expect(needed).toEqual({
      title: 'This has to be approved before anything moves',
      message:
        'Nothing moves until it is approved. The administrator who asked cannot approve it, unless this organization ' +
        'has been set up as having one administrator and nobody else administers it.',
      reasons,
    });
  });

  it('does not say who else approves: that is the organization’s to have set up', () => {
    for (const t of [inEnglish, inIndonesian]) {
      expect(approvalNeeded(plan({ requires_second_approver: true }), t)?.message).not.toMatch(/different administrator|administrator lain/);
    }
  });

  it('says so in Indonesian', () => {
    const needed = approvalNeeded(plan({ requires_second_approver: true }), inIndonesian);
    expect(needed?.title).toBe('Ini harus disetujui sebelum ada yang dipindahkan');
    expect(needed?.message).toBe(
      'Tidak ada yang dipindahkan sampai ini disetujui. Administrator yang memintanya tidak dapat menyetujuinya, ' +
        'kecuali organisasi ini telah diatur memiliki satu administrator dan tidak ada orang lain yang menjadi administratornya.',
    );
  });

  it('says so with no reasons when the server sends none, or sends an empty list', () => {
    expect(approvalNeeded(plan({ requires_second_approver: true }), inEnglish)?.reasons).toEqual([]);
    expect(approvalNeeded(plan({ requires_second_approver: true, second_approver_reasons: [] }), inEnglish)?.reasons).toEqual([]);
    expect(approvalNeeded(plan({ requires_second_approver: true, second_approver_reasons: ['', ' '] }), inEnglish)?.reasons).toEqual([]);
    // Not as the server writes it: read as no reasons, not thrown on.
    const odd = plan({ requires_second_approver: true, second_approver_reasons: 'a reason' as unknown as string[] });
    expect(approvalNeeded(odd, inEnglish)?.reasons).toEqual([]);
    const mixed = plan({ requires_second_approver: true, second_approver_reasons: [null, 'a reason', 4] as unknown as string[] });
    expect(approvalNeeded(mixed, inEnglish)?.reasons).toEqual(['a reason']);
  });

  it('says nothing for a plan one administrator can apply, or for no plan', () => {
    expect(approvalNeeded(plan(), inEnglish)).toBeNull();
    // A server older than the second approver sends no such field.
    expect(approvalNeeded(plan({ requires_second_approver: undefined }), inEnglish)).toBeNull();
    // Reasons with no flag ask nobody: the flag is the server's decision.
    expect(approvalNeeded(plan({ second_approver_reasons: reasons }), inEnglish)).toBeNull();
    expect(approvalNeeded(null, inEnglish)).toBeNull();
  });
});

describe('what the apply button says it will do', () => {
  it('says it will ask, not that it will move, when a second administrator is needed', () => {
    const asks = plan({ requires_second_approver: true });
    expect(applyLabel(asks, inEnglish)).toBe('Send for approval');
    expect(applyLabel(asks, inIndonesian)).toBe('Kirim untuk persetujuan');
  });

  it('says how many instances it moves for a plan one administrator can apply, in the reader’s language', () => {
    expect(applyLabel(plan(), inEnglish)).toBe('Move 3 instances');
    expect(applyLabel(plan({ instances: 1 }), inEnglish)).toBe('Move 1 instance');
    expect(applyLabel(plan({ instances: 0 }), inEnglish)).toBe('Move 0 instances');
    expect(applyLabel(null, inEnglish)).toBe('Move 0 instances');
    // In the catalogues now, as the button beside it and what it becomes
    // for a plan that waits are: an Indonesian dialog had this one button in
    // English.
    expect(applyLabel(plan(), inIndonesian)).toBe('Pindahkan 3 instansi');
    expect(applyLabel(plan({ instances: 1 }), inIndonesian)).toBe('Pindahkan 1 instansi');
  });
});
