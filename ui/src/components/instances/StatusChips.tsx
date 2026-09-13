import { Group, Paper, Text, UnstyledButton } from '@mantine/core';

import { STATUS } from '../statusVocabulary';
import type { StatusCount } from '../../domain/instanceList';

interface StatusChipsProps {
  /** Every state the project holds, already ordered by how much it matters. */
  counts: StatusCount[];
  /** How many instances the project holds across every state. */
  total: number;
  /** The state currently filtered to, or undefined for all of them. */
  selected?: string;
  onSelect: (status: string | undefined) => void;
  /** How many of the project's instances hold an unresolved incident. */
  needsAttentionTotal: number;
  needsAttentionSelected: boolean;
  onSelectNeedsAttention: (on: boolean) => void;
}

/**
 * The project's instances by state, as the control that filters them.
 *
 * Deliberately one thing and not two. The obvious design is a row of summary
 * tiles above a separate filter dropdown, and it is worse in both directions:
 * the tiles show a number nobody can act on, and the dropdown offers an action
 * with no number attached. Here the number *is* the button — "12 need
 * attention" is read and pressed in the same motion.
 *
 * The counts come from the server and describe the whole project, which is what
 * makes them worth showing at all. A count derived from the rows on screen
 * would say "0 need attention" on a page of completed runs, which is the one
 * wrong answer this page must never give.
 *
 * Rendered as radios rather than as buttons: they are one choice out of a fixed
 * set with exactly one always in effect, which is what a radio group is. As
 * buttons, a screen reader gets no indication of which is currently applied.
 */
export function StatusChips({
  counts,
  total,
  selected,
  onSelect,
  needsAttentionTotal,
  needsAttentionSelected,
  onSelectNeedsAttention,
}: StatusChipsProps) {
  // A chip that is switched on stays on screen even when its count has fallen
  // to zero. Empty states are dropped so the row is not padded with controls
  // that can only empty the table — but dropping the *selected* one hides the
  // reason the table is empty, and leaves a filter in force with nothing on
  // screen admitting it. That happens for real: choose "Need attention", then
  // choose a process that has none.
  const shown = selected && !counts.some((count) => count.status === selected)
    ? [...counts, { status: selected, total: 0 }]
    : counts;

  return (
    <Group
      gap="sm"
      wrap="wrap"
      role="radiogroup"
      aria-label="Filter instances by state"
    >
      <Chip
        label="All"
        count={total}
        color="gray"
        selected={selected === undefined && !needsAttentionSelected}
        onSelect={() => onSelect(undefined)}
      />

      {/*
        First, and only when there is something to say. This is not a status —
        an instance whose job ran out of retries is still `active` as far as the
        engine is concerned, and nothing ever writes a `failed` one — so a row
        of status chips alone can never answer "is anything broken?". It is the
        question this page is opened to ask, so its answer goes at the front.
      */}
      {(needsAttentionTotal > 0 || needsAttentionSelected) && (
        <Chip
          label="Need attention"
          count={needsAttentionTotal}
          color="red"
          selected={needsAttentionSelected}
          // Coloured even when it is not the current filter. Every other chip
          // is a neutral fact about the project and earns its colour by being
          // chosen; this one is the alarm, and an alarm that looks like the
          // rest of the row until you click it has already failed.
          alarm
          onSelect={() => onSelectNeedsAttention(!needsAttentionSelected)}
        />
      )}
      {shown.map((count) => {
        const presentation = STATUS[count.status.toLowerCase()];
        return (
          <Chip
            key={count.status}
            label={presentation?.label ?? count.status}
            count={count.total}
            color={presentation?.color ?? 'gray'}
            selected={selected === count.status && !needsAttentionSelected}
            // Pressing the state already in effect clears it. Without that the
            // only way back to the full list is the All chip, which is a second
            // thing to find for something the user just did.
            onSelect={() => onSelect(selected === count.status ? undefined : count.status)}
          />
        );
      })}
    </Group>
  );
}

interface ChipProps {
  label: string;
  count: number;
  color: string;
  selected: boolean;
  /** Carry the colour even when unselected. For the one chip that is a warning. */
  alarm?: boolean;
  onSelect: () => void;
}

function Chip({ label, count, color, selected, alarm = false, onSelect }: ChipProps) {
  const tinted = selected || alarm;
  return (
    <UnstyledButton
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      style={{ borderRadius: 'var(--mantine-radius-md)' }}
    >
      <Paper
        withBorder
        radius="md"
        px="md"
        py="xs"
        style={{
          // The selected chip is marked by its own accent colour rather than by
          // a fill: filling it would put a second strong colour beside the
          // status badges in the table, and those colours have to keep meaning
          // what they mean.
          borderColor: tinted
            ? `var(--mantine-color-${color}-filled)`
            : 'light-dark(var(--mantine-color-gray-3), var(--mantine-color-dark-4))',
          borderWidth: selected ? 2 : 1,
          // Padding compensates for the thicker border so nothing shifts by a
          // pixel as the selection moves along the row.
          paddingBlock: selected ? 'calc(var(--mantine-spacing-xs) - 1px)' : undefined,
          paddingInline: selected ? 'calc(var(--mantine-spacing-md) - 1px)' : undefined,
          background: selected ? `var(--mantine-color-${color}-light)` : undefined,
        }}
      >
        <Group gap="xs" wrap="nowrap" align="baseline">
          <Text fw={600} fz="lg" lh={1.2} c={tinted ? color : undefined}>
            {count.toLocaleString()}
          </Text>
          <Text size="sm" c={tinted ? color : 'dimmed'}>
            {label}
          </Text>
        </Group>
      </Paper>
    </UnstyledButton>
  );
}
