import { createTheme, rem, Card, Button, Table, Paper, ActionIcon, Badge, TextInput } from '@mantine/core';

/**
 * Design tokens for Metis BPM.
 *
 * These lived inside `routes/__root.tsx`. A route file is not importable as a
 * source of truth, so components hardcoded values instead of referencing it —
 * which is how 41 files ended up with literal pixel widths.
 *
 * Tailwind's `@theme` block (src/styles/tailwind.css) maps onto the CSS
 * variables Mantine generates from this, so there is exactly one place a
 * colour or spacing step is defined.
 */

/**
 * Typography.
 *
 * The previous scale set headings to weight 800 and labels to 700, with
 * uppercase and letter-spacing on table headers and stat titles. When
 * everything is emphasised nothing is: the eye has no way to find the one
 * important thing on the screen.
 *
 * Enterprise tools earn seriousness through restraint. 600 for headings, 500
 * for labels, and emphasis reserved for the single primary action on a view.
 */
const headings = {
  fontFamily: 'Inter, system-ui, sans-serif',
  fontWeight: '600',
  sizes: {
    h1: { fontSize: rem(28), lineHeight: '1.3', fontWeight: '600' },
    h2: { fontSize: rem(22), lineHeight: '1.35', fontWeight: '600' },
    h3: { fontSize: rem(18), lineHeight: '1.4', fontWeight: '600' },
    h4: { fontSize: rem(16), lineHeight: '1.45', fontWeight: '600' },
    h5: { fontSize: rem(14), lineHeight: '1.5', fontWeight: '600' },
    h6: { fontSize: rem(13), lineHeight: '1.5', fontWeight: '600' },
  },
};

export const theme = createTheme({
  primaryColor: 'blue',
  primaryShade: { light: 6, dark: 5 },
  defaultRadius: 'md',
  fontFamily: 'Inter, system-ui, sans-serif',
  fontFamilyMonospace: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, monospace',
  headings,

  /**
   * Shadows are deliberately shallow. Depth should communicate layering —
   * what floats above what — not decoration. Cards sit flat; only genuinely
   * floating surfaces (menus, modals, popovers) lift.
   */
  shadows: {
    xs: '0 1px 2px rgba(0, 0, 0, 0.04)',
    sm: '0 1px 3px rgba(0, 0, 0, 0.06), 0 1px 2px rgba(0, 0, 0, 0.04)',
    md: '0 4px 8px -2px rgba(0, 0, 0, 0.08), 0 2px 4px -2px rgba(0, 0, 0, 0.04)',
    lg: '0 12px 20px -6px rgba(0, 0, 0, 0.10), 0 4px 8px -4px rgba(0, 0, 0, 0.05)',
    xl: '0 24px 40px -12px rgba(0, 0, 0, 0.14)',
  },

  components: {
    Card: Card.extend({
      defaultProps: { withBorder: true, padding: 'lg', radius: 'md' },
      // Hover elevation is opt-in via data-interactive; see styles/tailwind.css.
      // Applying it to every card made read-only panels look clickable.
      classNames: { root: 'app-card' },
    }),

    Button: Button.extend({
      defaultProps: { radius: 'md', fw: 500 },
    }),

    Table: Table.extend({
      defaultProps: { verticalSpacing: 'sm', horizontalSpacing: 'md', highlightOnHover: true },
      styles: {
        th: {
          // Was uppercase + letter-spacing + weight 700 + dimmed, which is
          // both shouty and a contrast risk on a tinted header row. Sentence
          // case at normal weight reads faster and passes AA comfortably.
          fontSize: rem(12),
          fontWeight: 600,
          color: 'var(--mantine-color-text)',
        },
      },
    }),

    Paper: Paper.extend({
      defaultProps: { radius: 'md', withBorder: true },
    }),

    ActionIcon: ActionIcon.extend({
      defaultProps: { radius: 'md', variant: 'subtle' },
    }),

    Badge: Badge.extend({
      defaultProps: { radius: 'sm', fw: 600 },
    }),

    TextInput: TextInput.extend({
      defaultProps: { radius: 'md' },
    }),
  },
});
