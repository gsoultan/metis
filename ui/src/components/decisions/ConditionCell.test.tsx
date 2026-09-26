/**
 * One condition cell of the decision editor, rendered: what it marks as a
 * mistake, which is the underline an author sees while typing.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { ConditionCell } from './ConditionCell';

const headings = new Map([
  ['score', 'Score'],
  ['minimum', 'Minimum'],
]);
const cellProps = { 'data-row': 0, 'data-col': 0, onKeyDown: () => {}, onPaste: () => {} };

/** The cell's text box, as rendered. */
function conditionBox(value: string): string {
  const html = renderToStaticMarkup(
    <MantineProvider>
      <ConditionCell
        value={value}
        type="number"
        columnLabel="Score"
        headings={headings}
        cellProps={cellProps}
        onChange={() => {}}
      />
    </MantineProvider>,
  );
  return html.match(/<input[^>]*aria-label="Score condition"[^>]*>/)?.[0] ?? '';
}

/** The red wavy underline the cell draws under a condition it cannot read. */
const underlined = (box: string) => box.includes('underline wavy');

describe('a condition cell', () => {
  it('does not mark a comparison with another column as a mistake', () => {
    for (const value of ['> minimum', 'minimum', '[minimum..100]']) {
      const box = conditionBox(value);
      expect(box).not.toBe('');
      expect(underlined(box)).toBe(false);
      expect(box).not.toContain('aria-invalid');
    }
  });

  it('marks what the engine cannot read', () => {
    expect(underlined(conditionBox('? > minimum'))).toBe(true);
    expect(underlined(conditionBox('"GOLD'))).toBe(true);
  });

  // The underline is for eyes. A screen reader learns a field is wrong from
  // aria-invalid, which the cell set and Mantine replaced with its own, taken
  // from `error` — so no condition was ever announced as broken.
  it('says so to a screen reader too', () => {
    expect(conditionBox('? > minimum')).toContain('aria-invalid="true"');
    expect(conditionBox('"GOLD')).toContain('aria-invalid="true"');
  });
});
