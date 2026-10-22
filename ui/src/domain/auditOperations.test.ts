import { describe, expect, it } from 'bun:test';

import {
  AUDIT_EVENT_TYPES,
  auditQueryString,
  auditTypeLabel,
  endOfLocalDay,
  nextAuditCursor,
  operationsOf,
  shortId,
  startOfLocalDay,
} from './auditOperations';
import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { format } from '../i18n/translate';

describe('the audit log query string', () => {
  it('sends only the filters that carry a value, and always a limit', () => {
    expect(auditQueryString({})).toBe('?limit=50');
    expect(auditQueryString({ actor: '  ', type: undefined, project: '' })).toBe('?limit=50');
  });

  it('sends every filter and the cursor, trimmed and encoded', () => {
    const query = new URLSearchParams(
      auditQueryString(
        {
          actor: ' alice ',
          type: 'node_skipped',
          project: 'p-1',
          from: '2026-10-01T00:00:00.000Z',
          to: '2026-10-02T00:00:00.000Z',
        },
        'opaque+cursor/==',
        25,
      ),
    );
    expect(Object.fromEntries(query)).toEqual({
      actor: 'alice',
      type: 'node_skipped',
      project: 'p-1',
      from: '2026-10-01T00:00:00.000Z',
      to: '2026-10-02T00:00:00.000Z',
      before: 'opaque+cursor/==',
      limit: '25',
    });
  });

  it('keeps the page size inside what the server accepts', () => {
    expect(auditQueryString({}, undefined, 0)).toBe('?limit=1');
    expect(auditQueryString({}, undefined, 1000)).toBe('?limit=200');
  });
});

describe('paging', () => {
  it('stops when the server sends no cursor', () => {
    expect(nextAuditCursor({ operations: [], next: 'abc' })).toBe('abc');
    expect(nextAuditCursor({ operations: [] })).toBeUndefined();
    expect(nextAuditCursor({ operations: [], next: '' })).toBeUndefined();
  });

  it('joins the pages in the order they were read', () => {
    const op = (n: string) => ({ id: n, at: '', actor: 'a', type: 't', project_id: 'p', narrative: '' });
    expect(operationsOf([{ operations: [op('1'), op('2')] }, { operations: [op('3')] }]).map((o) => o.id)).toEqual([
      '1',
      '2',
      '3',
    ]);
  });
});

describe('a day picked on screen', () => {
  it('starts at local midnight, and an inclusive end runs to the next one', () => {
    const start = new Date(startOfLocalDay('2026-10-22') as string);
    expect([start.getFullYear(), start.getMonth(), start.getDate(), start.getHours()]).toEqual([2026, 9, 22, 0]);
    const end = new Date(endOfLocalDay('2026-10-22') as string);
    expect([end.getFullYear(), end.getMonth(), end.getDate(), end.getHours()]).toEqual([2026, 9, 23, 0]);
  });

  it('is nothing when nothing, or not a date, was picked', () => {
    expect(startOfLocalDay(null)).toBeUndefined();
    expect(endOfLocalDay('')).toBeUndefined();
    expect(startOfLocalDay('yesterday')).toBeUndefined();
  });
});

describe('what an event type is called', () => {
  it('has words for every offered type in every catalogue', () => {
    for (const catalogue of [en, id]) {
      for (const type of AUDIT_EVENT_TYPES) {
        expect(catalogue[`audit.type.${type}`]).toBeDefined();
      }
    }
  });

  it('falls back to the raw type, not the message key, for one it does not know', () => {
    const t = (key: string) => format(en, key);
    expect(auditTypeLabel('node_skipped', t)).toBe('Step waived');
    expect(auditTypeLabel('something_new', t)).toBe('something_new');
  });
});

describe('a short id', () => {
  it('is the first block of a UUID', () => {
    expect(shortId('0e4f2a1b-1111-2222-3333-444455556666')).toBe('0e4f2a1b');
    expect(shortId('plain')).toBe('plain');
  });
});
