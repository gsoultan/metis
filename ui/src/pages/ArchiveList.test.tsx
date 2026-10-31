/**
 * The archive's rows, rendered: the process by name and version, the case's
 * business key and how it ended, in words.
 */
import { describe, expect, it } from 'bun:test';

import type { ArchivedInstance } from '../domain/archive';
import { renderStatic, visibleText } from '../test/renderStatic';
import { ArchiveTable } from './ArchiveList';

const archived = (overrides: Partial<ArchivedInstance>): ArchivedInstance => ({
  id: 'i-1',
  project_id: 'p-1',
  definition_id: 'd-1',
  process_key: 'claims',
  process_name: 'Claims',
  process_version: 2,
  status: 'completed',
  started_at: '2026-01-01T09:00:00Z',
  finished_at: '2026-01-02T09:00:00Z',
  archived_at: '2026-04-02T09:00:00Z',
  ...overrides,
});

describe('the archive table', () => {
  it('draws each archived instance with its process, business key and outcome', async () => {
    const text = visibleText(
      await renderStatic(
        <ArchiveTable
          instances={[archived({ business_key: 'CL-77' }), archived({ id: 'i-2', status: 'cancelled', process_name: '' })]}
          onOpen={() => {}}
        />,
      ),
    );
    expect(text).toContain('Claims v2');
    expect(text).toContain('CL-77');
    expect(text).toContain('Completed');
    expect(text).toContain('claims v2');
    expect(text).toContain('Cancelled');
  });
});
