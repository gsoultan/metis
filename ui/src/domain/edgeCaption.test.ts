import { describe, expect, it } from 'bun:test';
import type { Edge } from '@xyflow/react';

import type { BPMNEdgeData } from '../types/bpmn';
import { editRawSettings } from './rawSettings';
import { edgeWithData } from './edgeCaption';

const conditioned: Edge<BPMNEdgeData> = {
  id: 'big',
  source: 'decide',
  target: 'director',
  label: 'amount > 1000',
  data: { condition: 'amount > 1000', documentation: '' },
};

/**
 * The arrow's caption is its condition, and follows it.
 *
 * Deleting an edge's condition in the raw editor left the old condition on
 * the arrow: the designer took the caption from the condition and, with none,
 * fell back to the caption the arrow already had. The canvas then said the
 * path was taken when amount > 1000, and it was taken always.
 */
describe('edgeWithData', () => {
  it('drops the caption with a condition deleted in the raw editor', () => {
    const { patch } = editRawSettings(conditioned.data ?? {}, JSON.stringify({ documentation: '' }));

    expect(edgeWithData(conditioned, patch ?? {}).label).toBe('');
  });

  it('drops the caption with a condition cleared in the condition builder', () => {
    expect(edgeWithData(conditioned, { ...conditioned.data, condition: '' }).label).toBe('');
  });

  it('captions the arrow with a new condition', () => {
    expect(edgeWithData(conditioned, { condition: 'amount > 5000' }).label).toBe('amount > 5000');
  });

  it('leaves the caption alone when the condition is left alone', () => {
    // A template captions its arrows "Approved" and "Rejected" and leaves the
    // conditions to the author, so a note added first keeps the caption.
    const suggested: Edge<BPMNEdgeData> = { id: 'yes', source: 'decide', target: 'approved', label: 'Approved' };

    expect(edgeWithData(suggested, { documentation: 'Approved by the manager' }).label).toBe('Approved');
    expect(edgeWithData(conditioned, { documentation: 'Large expenses' }).label).toBe('amount > 1000');
  });
});
