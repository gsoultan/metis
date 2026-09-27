import type { ClipboardEvent, KeyboardEvent } from 'react';

/**
 * What every grid cell needs to behave like a spreadsheet cell.
 *
 * Its position, so the keyboard can find its neighbours, plus the two handlers
 * that make a grid a grid: moving between cells, and accepting a block of them
 * off the clipboard.
 */
export interface GridCellProps {
  'data-row': number;
  'data-col': number;
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  onPaste: (event: ClipboardEvent<HTMLElement>) => void;
}
