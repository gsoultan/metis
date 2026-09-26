import { describe, expect, it } from 'bun:test';

import { migrationNotice } from './migrationOutcome';
import type { ApiMigrationPlan } from '../services/types';

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
