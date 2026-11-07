import { describe, expect, test } from 'bun:test';
import { behindRows, movable, moveSummary, versionBlocked } from './approvalTakeover';
import type { ApprovalTakeover } from '../services/domains/approvalLineService';

const takeover: ApprovalTakeover = {
  key: 'expense',
  live_version: 2,
  behind: [
    {
      version: 1,
      definition_id: 'd-1',
      plan: {},
      requests: [
        {
          instance_id: 'r-1', key: 'expense', version: 1, status: 'in_progress', submitted_by: 'rina',
          variables: { title: 'Taxi' },
          steps: [{ id: 'supervisor', name: 'Supervisor approval', status: 'waiting', waiting: { task_id: 't-1', users: ['budi'] } }],
        },
        {
          instance_id: 'r-2', key: 'expense', version: 1, status: 'sent_back', submitted_by: 'rina',
          revision: { task_id: 't-2', assignee: 'rina' },
          steps: [{ id: 'supervisor', name: 'Supervisor approval', status: 'sent_back' }],
        },
      ],
    },
    {
      version: 0,
      definition_id: 'd-0',
      plan: { refusals: ['step unit-head is gone'] },
      requests: [{ instance_id: 'r-3', key: 'expense', version: 0, status: 'in_progress', steps: [] }],
    },
  ],
};

describe('behindRows', () => {
  test('says where each request is and who it waits on', () => {
    const rows = behindRows(takeover);
    expect(rows.map((r) => r.id)).toEqual(['r-1', 'r-2', 'r-3']);
    expect(rows[0]).toMatchObject({ title: 'Taxi', where: 'Supervisor approval', waitsFor: 'budi', blocked: null });
    expect(rows[1]).toMatchObject({ where: 'Sent back to revise', waitsFor: 'rina' });
    expect(rows[2].blocked).toBe('step unit-head is gone');
  });

  test('no takeover is no rows', () => {
    expect(behindRows(undefined)).toEqual([]);
  });
});

describe('movable', () => {
  test('only the selected requests a move can take', () => {
    const rows = behindRows(takeover);
    expect(movable(rows, new Set(['r-1', 'r-3']))).toEqual(['r-1']);
  });
});

describe('versionBlocked', () => {
  test('a move that needs a second administrator goes to the designer', () => {
    expect(versionBlocked({ version: 1, definition_id: 'd', requests: [], plan: { requires_second_approver: true } })).toContain('second administrator');
  });
});

describe('moveSummary', () => {
  test('says what moved and what was left', () => {
    expect(moveSummary(1, 0, 2)).toBe("Moved 1 request to v2. Their open step goes to the new line's approvers.");
    expect(moveSummary(0, 2, 2)).toBe('Nothing was moved. 2 requests were left on their line, with the reason.');
  });
});
