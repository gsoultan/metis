/**
 * The way out of a legacy window, as it first appears.
 */
import { describe, expect, it } from 'bun:test';

import { renderMarkup } from '../../testing/renderMarkup';
import { visibleText } from '../../test/renderStatic';
import { CloseLegacyWindow } from './CloseLegacyWindow';

describe('CloseLegacyWindow', () => {
  it('says what waiting costs, and offers to stop now', () => {
    const text = visibleText(renderMarkup(<CloseLegacyWindow hookId="w-1" deadline="Dec 25, 2026, 9:00 AM" />));
    expect(text).toContain('rather than on Dec 25, 2026, 9:00 AM');
    expect(text).toContain('a captured delivery signed the old way is still acted on');
    expect(text).toContain('Stop accepting legacy signatures now');
    // Nothing is refused until the second step.
    expect(text).not.toContain('Stop now');
  });
});
