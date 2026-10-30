import { describe, expect, it } from 'bun:test';

import { archiveQueryString, processLabel, timelineEntriesOf } from './archive';

describe('the archive query string', () => {
  it('sends only the filters that are filled in, trimmed, with the cursor and the page size', () => {
    expect(archiveQueryString({})).toBe('?limit=50');
    expect(archiveQueryString({ processKey: '  claims ', businessKey: '' }, 'c1', 10)).toBe(
      '?process_key=claims&cursor=c1&limit=10',
    );
    expect(archiveQueryString({ businessKey: 'PO 1&2' })).toBe('?business_key=PO+1%262&limit=50');
  });
});

describe('an archived trail', () => {
  it('reads as the live timeline reads one, in the same order', () => {
    const entries = timelineEntriesOf([
      { id: 'a', created_at: '2026-01-01T00:00:00Z', type: 'process_started', message: 'started' },
      {
        id: 'b',
        created_at: '2026-01-01T00:01:00Z',
        type: 'node_reached',
        message: 'reached',
        narrative: 'The request reached Approve',
        node_id: 'approve',
        node_name: 'Approve',
      },
    ]);
    expect(entries.map((e) => e.id)).toEqual(['a', 'b']);
    expect(entries[1]).toMatchObject({
      timestamp: '2026-01-01T00:01:00Z',
      narrative: 'The request reached Approve',
      node: { id: 'approve', name: 'Approve' },
    });
    expect(entries[0].node).toBeUndefined();
    expect(timelineEntriesOf(null)).toEqual([]);
  });
});

describe('an archived instance’s process', () => {
  it('is named by its name and version, or by its key when it had no name', () => {
    expect(processLabel({ process_name: 'Claims', process_key: 'claims', process_version: 3 })).toBe('Claims v3');
    expect(processLabel({ process_name: '', process_key: 'claims', process_version: 1 })).toBe('claims v1');
  });
});
