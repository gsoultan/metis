import { describe, expect, it } from 'bun:test';

import {
  buildDraft,
  describeDraftAge,
  draftKey,
  headerStatus,
  isWorthSaving,
  parseDraft,
  shouldOfferDraft,
} from './designerDraft';

describe('where a draft is kept', () => {
  it('keys an unsaved process separately from a deployed one', () => {
    expect(draftKey(null)).toBe('metis_draft_new');
    expect(draftKey('abc')).toBe('metis_draft_abc');
  });
});

describe('what is worth keeping', () => {
  it('does not treat an empty canvas as unsaved work', () => {
    expect(isWorthSaving([])).toBe(false);
    expect(isWorthSaving([{ id: 'n1' }])).toBe(true);
  });
});

describe('reading a stored draft', () => {
  it('reads back what it wrote', () => {
    const draft = buildDraft({
      definitionId: 'abc',
      processName: 'Expense approval',
      processKey: 'expense-approval',
      nodes: [{ id: 'n1' }],
      edges: [{ id: 'f1' }],
      savedAt: new Date('2026-09-06T10:00:00Z'),
    });
    const read = parseDraft(JSON.stringify(draft));
    expect(read).not.toBeNull();
    expect(read?.processName).toBe('Expense approval');
    expect(read?.nodes).toHaveLength(1);
    expect(read?.savedAt).toBe('2026-09-06T10:00:00.000Z');
  });

  it('reads a draft written by the version that used `timestamp`', () => {
    const legacy = JSON.stringify({
      nodes: [{ id: 'n1' }],
      edges: [],
      processName: 'Old',
      processKey: 'old',
      timestamp: '2026-09-06T09:00:00.000Z',
    });
    expect(parseDraft(legacy)?.savedAt).toBe('2026-09-06T09:00:00.000Z');
  });

  /*
   * Storage is shared with every other tab and version on the origin. A value
   * that is not a draft must read as "no draft", never throw: this runs during
   * a route transition, where an exception is a blank screen.
   */
  it('treats anything that is not a draft as no draft', () => {
    expect(parseDraft(null)).toBeNull();
    expect(parseDraft('')).toBeNull();
    expect(parseDraft('not json at all')).toBeNull();
    expect(parseDraft('"a string"')).toBeNull();
    expect(parseDraft('{"nodes":"not an array"}')).toBeNull();
    expect(parseDraft('{"nodes":[],"edges":[]}')).toBeNull(); // no timestamp
  });
});

describe('whether to offer it back', () => {
  const draftAt = (iso: string) =>
    buildDraft({
      definitionId: 'abc',
      processName: 'p',
      processKey: 'p',
      nodes: [{ id: 'n1' }],
      edges: [],
      savedAt: new Date(iso),
    });

  it('offers a draft newer than what the server returned', () => {
    expect(shouldOfferDraft(draftAt('2026-09-06T12:00:00Z'), '2026-09-06T11:00:00Z')).toBe(true);
  });

  /*
   * A draft older than the deployed version is work that was already published,
   * or that somebody else has since superseded. Restoring it would quietly undo
   * a colleague, which is worse than losing it.
   */
  it('does not offer a draft older than the deployed version', () => {
    expect(shouldOfferDraft(draftAt('2026-09-06T10:00:00Z'), '2026-09-06T11:00:00Z')).toBe(false);
  });

  it('offers a draft for a process that was never deployed', () => {
    expect(shouldOfferDraft(draftAt('2026-09-06T10:00:00Z'), null)).toBe(true);
  });

  it('offers nothing when there is no draft or it is empty', () => {
    expect(shouldOfferDraft(null, null)).toBe(false);
    const empty = { ...draftAt('2026-09-06T10:00:00Z'), nodes: [] };
    expect(shouldOfferDraft(empty, null)).toBe(false);
  });
});

describe('how old it is, in words', () => {
  const now = new Date('2026-09-06T12:00:00Z');
  const at = (iso: string) =>
    buildDraft({ processName: 'p', processKey: 'p', nodes: [], edges: [], savedAt: new Date(iso) });

  it('says it in language a person uses', () => {
    expect(describeDraftAge(at('2026-09-06T11:59:40Z'), now)).toBe('from a moment ago');
    expect(describeDraftAge(at('2026-09-06T11:59:00Z'), now)).toBe('from a minute ago');
    expect(describeDraftAge(at('2026-09-06T11:30:00Z'), now)).toBe('from 30 minutes ago');
    expect(describeDraftAge(at('2026-09-06T11:00:00Z'), now)).toBe('from an hour ago');
    expect(describeDraftAge(at('2026-09-06T06:00:00Z'), now)).toBe('from 6 hours ago');
    expect(describeDraftAge(at('2026-09-05T10:00:00Z'), now)).toBe('from yesterday');
    expect(describeDraftAge(at('2026-09-01T10:00:00Z'), now)).toBe('from 5 days ago');
  });
});

describe('what the designer header says', () => {
  const at = new Date('2026-11-14T02:00:00Z');

  it('names the version of a deployed process with nothing unsaved', () => {
    expect(headerStatus(null, 3)).toBe('Version 3 · no unsaved changes');
  });

  it('says a process was never deployed only when it was not', () => {
    expect(headerStatus(null, null)).toBe('Not deployed yet');
  });

  it('says which version kept changes belong to', () => {
    expect(headerStatus(at, 3)).toStartWith('Changes to version 3 kept in this browser at ');
    expect(headerStatus(at, null)).toStartWith('Draft kept in this browser at ');
  });
});
