import { describe, expect, it } from 'bun:test';

import { blankDraft, draftFor, editDraft, mappingOf, proposedRows, versionPair } from './migrationDraft';
import type { MigrationDraft } from './migrationDraft';

// Somebody plans moving v2's work onto v5: maps the removed approval onto
// sales, accepts losing its control, and decides to skip a second step. Then
// they cancel, and open the same dialog for v3's work instead.
const v2ToV5 = versionPair('def-2', 'def-5');
const v3ToV5 = versionPair('def-3', 'def-5');
const written: MigrationDraft = {
  pair: v2ToV5,
  rows: [{ id: 1, from: 'opsApprove', to: 'salesApprove' }],
  accepted: ['opsApprove'],
  actions: [{ from: 'legalReview', kind: 'skip', reason: 'role eliminated' }],
};

describe('draftFor', () => {
  it('does not carry a draft over to another pair of versions', () => {
    // The mapping, the acknowledgement and the skip were all about v2. Shown
    // for v3 they would be sent with v3's plan — an acknowledgement nobody
    // gave, and a skip nobody chose.
    expect(draftFor(written, v3ToV5)).toEqual(blankDraft(v3ToV5));
  });

  it('keeps the draft for the pair it was written for', () => {
    expect(draftFor(written, v2ToV5)).toBe(written);
  });

  it('starts clean when there is nothing written', () => {
    // What the dialog holds after it has been closed.
    expect(draftFor(null, v2ToV5)).toEqual(blankDraft(v2ToV5));
  });
});

describe('editDraft', () => {
  const proposal = proposedRows({ opsApprove: 'operationsApprove' });

  it('shows the proposed mapping until somebody edits it, and theirs after', () => {
    const blank = blankDraft(v2ToV5);
    expect(mappingOf(blank, proposal)).toEqual(proposal);
    const cleared = editDraft(blank, { type: 'mapping', change: () => [] }, proposal);
    // Removing the proposed row is an answer; the proposal does not come back.
    expect(mappingOf(cleared, proposal)).toEqual([]);
  });

  it('drops acknowledgements when the mapping they were given for changes', () => {
    const accepted = editDraft(blankDraft(v2ToV5), { type: 'accept', nodeId: 'opsApprove', accepted: true }, proposal);
    expect(accepted.accepted).toEqual(['opsApprove']);
    const remapped = editDraft(accepted, { type: 'mapping', change: () => [{ id: 2, from: 'opsApprove', to: 'x' }] }, proposal);
    expect(remapped.accepted).toEqual([]);
  });

  it('pins the mapping that was on screen when somebody acted on it', () => {
    // An acknowledgement is of the plan in front of the person. A proposal
    // that changes afterwards must not change what they acknowledged.
    const accepted = editDraft(blankDraft(v2ToV5), { type: 'accept', nodeId: 'opsApprove', accepted: true }, proposal);
    expect(mappingOf(accepted, proposedRows({ opsApprove: 'elsewhere' }))).toEqual(proposal);
  });

  it('keeps acknowledgements across a change to the decisions', () => {
    const accepted = editDraft(blankDraft(v2ToV5), { type: 'accept', nodeId: 'opsApprove', accepted: true }, proposal);
    const decided = editDraft(accepted, { type: 'actions', change: () => written.actions }, proposal);
    expect(decided.accepted).toEqual(['opsApprove']);
    expect(decided.actions).toEqual(written.actions);
  });
});
