/**
 * Rendering a long list of table rows without rendering all of them.
 *
 * A task inbox is fine at fifty rows and unusable at ten thousand: every row is
 * a handful of DOM nodes, and the browser lays out all of them on every filter
 * keystroke. Virtualization renders the twenty or so a person can see and leaves
 * space where the rest would be.
 *
 * Three decisions keep this from being a risky change to a real `<table>`:
 *
 * The scroll container stays *outside* the table, and is passed in. A `<table>`
 * computes its column widths from its rows, and the two usual ways to make rows
 * scrollable — `display: block` on the tbody, or taking rows out of flow to
 * position them — both detach that computation, so columns drift as you scroll.
 * Scrolling the wrapper leaves the table an ordinary table.
 *
 * Spacer rows stand in for what is above and below. Two empty rows with a height
 * keep the scrollbar honest without any row leaving normal flow.
 *
 * Below a threshold it does nothing at all. Most lists are short, the common
 * case renders through the same path it always has, and the machinery only
 * engages where it earns its keep.
 */
import { Table } from '@mantine/core';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { ReactNode, RefObject } from 'react';

/**
 * Below this many rows, nothing is virtualized.
 *
 * A hundred rows is more than anyone scrolls through and well inside what a
 * browser lays out without noticing. The point of the threshold is that the
 * ordinary case never touches this code.
 */
export const VIRTUALIZE_ABOVE = 100;

/** Roughly how tall a row is, before any has been measured. */
const ESTIMATED_ROW_HEIGHT = 72;

/** How many rows to render beyond the visible ones, so scrolling is not blank. */
const OVERSCAN = 8;

export function VirtualRows<T>({
  items,
  renderRow,
  columnCount,
  scrollRef,
}: {
  items: T[];
  renderRow: (item: T, index: number) => ReactNode;
  /** How many cells a spacer row must span to keep the table's shape. */
  columnCount: number;
  /**
   * The element that scrolls — the wrapper around the table, not the table.
   * Required, because virtualizing against the wrong element silently renders
   * the first twenty rows and nothing else.
   */
  scrollRef: RefObject<HTMLDivElement | null>;
}) {
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: OVERSCAN,
    // Disabled below the threshold so the hook still runs — hooks cannot be
    // called conditionally — while doing no work.
    enabled: items.length > VIRTUALIZE_ABOVE,
  });

  // The short case, which is nearly every case: no spacers, no measurement, the
  // same rows this table has always rendered.
  if (items.length <= VIRTUALIZE_ABOVE) {
    return <Table.Tbody>{items.map((item, index) => renderRow(item, index))}</Table.Tbody>;
  }

  const virtualRows = virtualizer.getVirtualItems();
  if (virtualRows.length === 0) {
    // Before the scroll element has been measured. Rendering the first screenful
    // rather than nothing means the list is never briefly empty.
    return <Table.Tbody>{items.slice(0, OVERSCAN * 3).map((item, index) => renderRow(item, index))}</Table.Tbody>;
  }

  const before = virtualRows[0].start;
  const after = virtualizer.getTotalSize() - virtualRows[virtualRows.length - 1].end;

  return (
    <Table.Tbody>
      {before > 0 && (
        <Table.Tr aria-hidden style={{ height: before }}>
          <Table.Td colSpan={columnCount} p={0} style={{ border: 'none' }} />
        </Table.Tr>
      )}

      {virtualRows.map((virtualRow) => renderRow(items[virtualRow.index], virtualRow.index))}

      {after > 0 && (
        <Table.Tr aria-hidden style={{ height: after }}>
          <Table.Td colSpan={columnCount} p={0} style={{ border: 'none' }} />
        </Table.Tr>
      )}
    </Table.Tbody>
  );
}
