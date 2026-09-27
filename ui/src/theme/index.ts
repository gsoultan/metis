import {
  defaultVariantColorsResolver,
  parseThemeColor,
  type CSSVariablesResolver,
  type VariantColorsResolver,
  createTheme, rem, Card, Button, Table, Paper, ActionIcon, Badge, TextInput, ThemeIcon } from '@mantine/core';
import { DIMMED_DARK, DIMMED_LIGHT } from './palette';

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

/**
 * Secondary text, dark enough to read.
 *
 * Mantine's default dimmed grey measures 3.15:1 against the application
 * background, where WCAG AA asks for 4.5:1 — it is chosen against pure white,
 * and the page is slightly grey. Every `c="dimmed"` in the product inherited
 * it, so an accessibility scan reported the same failure on every screen.
 *
 * Done through the resolver rather than a CSS rule of our own: Mantine's
 * stylesheet is imported after ours, so an equally specific `:root` override
 * loses on order and silently does nothing. The values are asserted against the
 * same surfaces in contrast.test.ts.
 */
/**
 * Variant colours, darkened where they carry text.
 *
 * Mantine computes a Badge's or Button's colours per element and writes them as
 * inline custom properties, so overriding the palette's CSS variables does not
 * reach them — the accessibility scan kept reporting the same blue after the
 * variables were fixed. This is the seam that does reach them.
 *
 * Shade 6 is Mantine's default for a filled variant's background, and fails AA
 * at ordinary text sizes: white on blue-6 measures 3.55:1, against 4.5:1.
 * Shade 8 clears it, in both colour schemes — blue-7, the obvious first guess,
 * still measures 4.19:1 under white.
 *
 * What this writes is written for both schemes at once: an inline property has
 * no colour scheme. A fixed shade is right only where it reads on both, which
 * is why a light variant's text is left to Mantine's own variable. That one
 * follows the scheme — shade 9 on the light tint (see readableColourVariables),
 * shade 0 on the dark one — where a fixed shade 9 left the webhooks card's
 * message badges at 2.14:1 in dark mode.
 */
export const variantColorResolver: VariantColorsResolver = (input) => {
  const resolved = defaultVariantColorsResolver(input);
  const parsed = parseThemeColor({
    color: input.color || input.theme.primaryColor,
    theme: input.theme,
  });

  // A colour given as a raw value rather than a palette name has no shades to
  // step through; leave it exactly as asked for.
  if (!parsed.isThemeColor || !parsed.color) {
    return resolved;
  }

  if (input.variant === 'filled') {
    return {
      ...resolved,
      background: `var(--mantine-color-${parsed.color}-8)`,
      hover: `var(--mantine-color-${parsed.color}-9)`,
    };
  }
  return resolved;
};

export const cssVariablesResolver: CSSVariablesResolver = () => ({
  variables: {},
  light: {
    '--mantine-color-dimmed': DIMMED_LIGHT,
    ...readableColourVariables(),
  },
  dark: { '--mantine-color-dimmed': DIMMED_DARK },
});

/**
 * The semantic colours, darkened where they carry text.
 *
 * Mantine's shade 6 is its default for both a light-variant badge's text and a
 * filled button's background. Both fail AA at ordinary text sizes: blue-6 on
 * blue-0 measures 3.78:1, and white on blue-6 measures 3.55:1, against the
 * 4.5:1 an accessibility scan asks for. A red "Delete" label on white was worse
 * at 3.28:1 — the one control where being sure of what you clicked matters most.
 *
* Shade 9 for text on a tint and shade 8 for a filled surface clear the
 * threshold — white on blue-7 is still only 4.19:1, one step short — and are
 * recognisably the same colours. Written as
 * references to Mantine's own palette rather than as hex, so a palette change
 * carries through instead of drifting from a copy.
 */
function readableColourVariables(): Record<string, string> {
  const semantic = ['blue', 'indigo', 'red', 'orange', 'green', 'teal', 'grape', 'yellow', 'cyan', 'violet', 'pink'];
  const variables: Record<string, string> = {};
  for (const colour of semantic) {
    variables[`--mantine-color-${colour}-text`] = `var(--mantine-color-${colour}-9)`;
    variables[`--mantine-color-${colour}-light-color`] = `var(--mantine-color-${colour}-9)`;
    variables[`--mantine-color-${colour}-filled`] = `var(--mantine-color-${colour}-8)`;
    variables[`--mantine-color-${colour}-filled-hover`] = `var(--mantine-color-${colour}-9)`;
  }
  return variables;
}

/**
 * The variant a filled-by-default component is given by name.
 *
 * "filled" is already Mantine's default for a Button, a Badge and a ThemeIcon,
 * but Mantine writes their colours from the variant colour resolver only when
 * a colour or a variant is passed. Given neither, the stylesheet falls back to
 * the scheme's primary filled colour — blue-5 in dark mode — under the white
 * label chosen against the light scheme's shade: the webhooks card's "Add a
 * webhook" measured 2.99:1. Naming the variant sends every one through the
 * resolver, which fills with shade 8 in both schemes: 5.02:1.
 *
 * The dark scheme's primary shade itself stays at 5. It is also the focus ring
 * and the fill of a switch, a checkbox and a progress bar, which carry no text
 * and must stand out from a dark card: 4.54:1 at shade 5, 2.70:1 at shade 8.
 */
const FILLED_THROUGH_THE_RESOLVER = { variant: 'filled' } as const;

export const theme = createTheme({
  primaryColor: 'blue',
  primaryShade: { light: 8, dark: 5 },
  // Picks black or white text per background luminance, so a colour this theme
  // does not name explicitly still gets readable text on it.
  autoContrast: true,
  variantColorResolver,
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
      defaultProps: { radius: 'md', fw: 500, ...FILLED_THROUGH_THE_RESOLVER },
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
      defaultProps: { radius: 'sm', fw: 600, ...FILLED_THROUGH_THE_RESOLVER },
    }),

    ThemeIcon: ThemeIcon.extend({
      defaultProps: FILLED_THROUGH_THE_RESOLVER,
    }),

    TextInput: TextInput.extend({
      defaultProps: { radius: 'md' },
    }),
  },
});
