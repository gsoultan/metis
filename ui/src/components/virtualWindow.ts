/**
 * The one place the app asks TanStack Virtual for a window onto a long list.
 *
 * The table rows of the task inbox and the columns of the decision graph both
 * draw only what is in view. They share this so that the library is adapted in
 * one place: its virtualizer hands back functions the React Compiler cannot
 * memoise safely (react-hooks/incompatible-library), and that is said once,
 * here, rather than at every list that scrolls.
 */
import { useVirtualizer } from '@tanstack/react-virtual';
import type { RefObject } from 'react';

export interface VirtualWindowOptions {
  count: number;
  /** The element that scrolls. */
  scrollRef: RefObject<HTMLElement | null>;
  /** Roughly how tall an item is, before any has been measured. */
  estimatedSize: number;
  /** How many items to draw beyond the visible ones, so scrolling is not blank. */
  overscan: number;
  /** False below the length worth windowing: the hook still runs, doing no work. */
  enabled: boolean;
}

export function useVirtualWindow({ count, scrollRef, estimatedSize, overscan, enabled }: VirtualWindowOptions) {
  return useVirtualizer({
    count,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => estimatedSize,
    overscan,
    enabled,
  });
}
