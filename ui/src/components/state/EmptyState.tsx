import { Button, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

interface EmptyStateProps {
  icon: LucideIcon;
  /** What is absent, in the user's words. "No processes yet", not "0 results". */
  title: string;
  /** Why they might want one, and what happens if they make one. */
  description?: string;
  /** The single next step. An empty state with no action is a dead end. */
  action?: ReactNode;
  /** Use when the emptiness is the result of a filter, not of having nothing. */
  variant?: 'blank' | 'filtered';
  size?: 'sm' | 'md';
}

/**
 * The state a surface shows when it has no rows.
 *
 * An empty state is the most-seen screen in a new account and the least
 * designed one in most products. Three rules it encodes:
 *
 *  1. **Empty is not an error.** This renders in neutral tones. The previous
 *     code used a red badge with a warning icon for "No Projects Found", which
 *     tells a new user their installation is broken on first launch.
 *
 *  2. **Say what this surface is for.** Someone landing here for the first time
 *     does not yet know what a "process definition" is or why they want one.
 *
 *  3. **Offer exactly one next step.** Two competing buttons on an empty screen
 *     is a decision the user has no information to make.
 *
 * `variant="filtered"` distinguishes "you have nothing" from "your filter
 * matched nothing" — the same visual with a different, and much more useful,
 * message.
 */
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  variant = 'blank',
  size = 'md',
}: EmptyStateProps) {
  const compact = size === 'sm';

  return (
    <Stack
      align="center"
      justify="center"
      gap={compact ? 'xs' : 'sm'}
      py={compact ? 'xl' : 64}
      px="md"
      // The role is status rather than alert: this is information, not a
      // problem, so it should not interrupt a screen reader mid-sentence.
      role="status"
    >
      <ThemeIcon
        size={compact ? 40 : 56}
        radius="xl"
        variant="light"
        color="gray"
        aria-hidden
      >
        <Icon size={compact ? 20 : 26} strokeWidth={1.75} />
      </ThemeIcon>

      <Title order={compact ? 5 : 4} ta="center" mt={compact ? 4 : 'xs'}>
        {title}
      </Title>

      {description && (
        <Text c="dimmed" size="sm" ta="center" maw={420} lh={1.6}>
          {description}
        </Text>
      )}

      {action && <div style={{ marginTop: compact ? 4 : 8 }}>{action}</div>}

      {variant === 'filtered' && !action && (
        <Text c="dimmed" size="xs" ta="center">
          Try removing a filter to see more.
        </Text>
      )}
    </Stack>
  );
}

interface EmptyStateActionProps {
  onClick: () => void;
  children: ReactNode;
  icon?: LucideIcon;
}

/** The primary action inside an EmptyState, styled consistently. */
export function EmptyStateAction({ onClick, children, icon: Icon }: EmptyStateActionProps) {
  return (
    <Button onClick={onClick} leftSection={Icon ? <Icon size={16} /> : undefined}>
      {children}
    </Button>
  );
}
