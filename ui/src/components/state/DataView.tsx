import type { ReactElement, ReactNode } from 'react';
import { EmptyState } from './EmptyState';
import { ErrorState } from './ErrorState';
import type { LucideIcon } from 'lucide-react';

interface DataViewProps<T> {
  isLoading: boolean;
  error?: unknown;
  data: T[] | undefined;
  /** Skeleton matching the shape of what is coming. */
  loading: ReactElement;
  /** Rendered once there is at least one row. */
  children: (rows: T[]) => ReactNode;

  emptyIcon: LucideIcon;
  emptyTitle: string;
  emptyDescription?: string;
  emptyAction?: ReactNode;

  /** True when a filter or search is active, so "empty" gets honest wording. */
  isFiltered?: boolean;
  filteredTitle?: string;
  filteredDescription?: string;

  /** Named in the user's terms: "load your tasks". */
  errorAction?: string;
  onRetry?: () => void;
}

/**
 * Resolves the four states every data surface has: loading, error, empty, and
 * content.
 *
 * The audit found 3 of 18 pages handling loading and none handling error, not
 * because anyone decided to skip them but because each page had to remember
 * four branches independently. Eighteen chances to forget, times three states,
 * is why fifteen pages render nothing while fetching.
 *
 * Making the states a *parameter* rather than a convention means a page cannot
 * silently omit one: `loading` and the empty copy are required props.
 *
 * Order matters. Error is checked before empty, because a failed request also
 * produces an empty array — and telling a user "no tasks yet" when the truth is
 * "we could not reach the server" is the difference between a calm screen and a
 * wrong one.
 */
export function DataView<T>({
  isLoading,
  error,
  data,
  loading,
  children,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  emptyAction,
  isFiltered = false,
  filteredTitle,
  filteredDescription,
  errorAction,
  onRetry,
}: DataViewProps<T>) {
  if (isLoading) {
    return loading;
  }

  // Before the empty check: a rejected request leaves `data` undefined, which
  // would otherwise render as "you have nothing".
  if (error) {
    return <ErrorState error={error} action={errorAction} onRetry={onRetry} />;
  }

  const rows = data ?? [];

  if (rows.length === 0) {
    return isFiltered ? (
      <EmptyState
        icon={emptyIcon}
        title={filteredTitle ?? 'Nothing matches your filters'}
        description={filteredDescription ?? 'Adjust or clear the filters to see more.'}
        variant="filtered"
      />
    ) : (
      <EmptyState
        icon={emptyIcon}
        title={emptyTitle}
        description={emptyDescription}
        action={emptyAction}
      />
    );
  }

  return <>{children(rows)}</>;
}
