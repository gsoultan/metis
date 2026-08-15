import { Box, Group, Stack, Text, Title } from '@mantine/core';
import type { ReactNode } from 'react';

/**
 * Page layout primitives.
 *
 * The visual language here is deliberate and worth stating, because "clean" is
 * a set of decisions rather than a taste:
 *
 *  - **Separate with borders and space, not shadows.** Shadows imply elevation,
 *    which should mean "this floats above the page" — menus, modals, popovers.
 *    Using them for ordinary panels makes a flat list of information look like
 *    a pile of floating cards.
 *  - **One accent colour.** Blue means "you can act on this". Everything else
 *    is neutral, except semantic status (red/amber/green), which must stay
 *    reserved for status or it stops meaning anything.
 *  - **Consistent vertical rhythm.** One spacing scale, applied in one place,
 *    so pages do not each invent their own margins.
 *  - **Hierarchy through size and weight, not decoration.** No gradient text,
 *    no uppercase tracking, no weight 800.
 */

interface PageShellProps {
  children: ReactNode;
}

/** Vertical rhythm for a page's sections. */
export function PageShell({ children }: PageShellProps) {
  return <Stack gap="xl">{children}</Stack>;
}

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: ReactNode;
  /** Optional status or count shown beside the title. */
  meta?: ReactNode;
}

/**
 * The page title block.
 *
 * The previous implementation pulled itself out of the page flow with
 * `margin: 0 -24px 32px -24px` — a hack that silently assumed its parent had
 * exactly 24px of padding — and painted a hardcoded white background with a
 * hardcoded grey border, so it stayed light while the rest of the app went
 * dark. It also rendered every title as gradient text at weight 800.
 */
export function PageHeader({ title, description, actions, meta }: PageHeaderProps) {
  return (
    <Box
      component="header"
      pb="lg"
      style={{
        // light-dark() works now that postcss-preset-mantine is installed;
        // previously every surface hand-rolled a `theme === 'dark' ? …` ternary.
        borderBottom: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
      }}
    >
      <Group justify="space-between" align="flex-start" wrap="nowrap" gap="md">
        <Stack gap={6} style={{ minWidth: 0 }}>
          <Group gap="sm" align="center" wrap="nowrap">
            <Title order={1}>{title}</Title>
            {meta}
          </Group>
          {description && (
            <Text c="dimmed" size="sm" lh={1.6}>
              {description}
            </Text>
          )}
        </Stack>

        {actions && (
          <Group gap="sm" wrap="nowrap" style={{ flexShrink: 0 }}>
            {actions}
          </Group>
        )}
      </Group>
    </Box>
  );
}

interface SectionProps {
  title?: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
}

/** A titled grouping within a page. */
export function Section({ title, description, actions, children }: SectionProps) {
  return (
    <Stack gap="md" component="section">
      {(title || actions) && (
        <Group justify="space-between" align="flex-end" wrap="nowrap" gap="md">
          <Stack gap={2} style={{ minWidth: 0 }}>
            {title && <Title order={3}>{title}</Title>}
            {description && (
              <Text c="dimmed" size="sm">
                {description}
              </Text>
            )}
          </Stack>
          {actions && (
            <Group gap="xs" wrap="nowrap" style={{ flexShrink: 0 }}>
              {actions}
            </Group>
          )}
        </Group>
      )}
      {children}
    </Stack>
  );
}
