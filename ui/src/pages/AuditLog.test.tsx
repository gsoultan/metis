/**
 * The audit log's rows, rendered: the sentence the server wrote, the type in
 * words (or its raw name when there are none), and a way into the instance
 * only where the operation touched one.
 */
import { describe, expect, it } from 'bun:test';

import type { AuditOperation } from '../domain/auditOperations';
import { renderStatic, visibleText } from '../test/renderStatic';
import { AuditTable } from './AuditLog';

const operation = (overrides: Partial<AuditOperation>): AuditOperation => ({
  id: 'op-1',
  at: '2026-10-22T10:00:00.123456Z',
  actor: 'alice',
  type: 'node_skipped',
  project_id: 'p-1',
  narrative: 'alice waived “Approve the refund”',
  ...overrides,
});

describe('the audit log table', () => {
  it('draws who, what and the narrative for each operation', async () => {
    const text = visibleText(
      await renderStatic(
        <AuditTable
          operations={[
            operation({ instance_id: '0e4f2a1b-1111-2222-3333-444455556666' }),
            operation({ id: 'op-2', actor: 'bob', type: 'brand_new_type', narrative: 'bob did a new thing' }),
          ]}
          projectNames={new Map([['p-1', 'Refunds']])}
        />,
      ),
    );
    expect(text).toContain('alice');
    expect(text).toContain('Step waived');
    expect(text).toContain('alice waived “Approve the refund”');
    expect(text).toContain('Refunds');
    expect(text).toContain('0e4f2a1b');
    expect(text).toContain('bob did a new thing');
    // A type the catalogue has no words for is shown by its name, not its key.
    expect(text).toContain('brand_new_type');
    expect(text).not.toContain('audit.type.');
  });

  it('offers the instance only where the operation touched one', async () => {
    const html = await renderStatic(<AuditTable operations={[operation({})]} projectNames={new Map()} />);
    expect(html).not.toContain('Open instance');
  });
});
