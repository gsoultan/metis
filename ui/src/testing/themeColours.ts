/**
 * The colours the theme gives the page, in either colour scheme, as hex.
 *
 * A control's colours reach the page as CSS custom properties — a Button's
 * `--button-bg: var(--mantine-color-blue-8)` — and what that variable means
 * depends on the colour scheme the page is in. This follows the chain the way
 * the browser would, from the variables the provider writes for a scheme, so a
 * test can measure the contrast a control will actually have without a
 * browser.
 */
import { DEFAULT_THEME, deepMerge, defaultCssVariablesResolver, mergeMantineTheme } from '@mantine/core';

import { cssVariablesResolver, theme } from '../theme';

export type ColourScheme = 'light' | 'dark';

/** Every variable the page defines in a scheme: Mantine's, then the theme's over them. */
export function schemeVariables(scheme: ColourScheme): Record<string, string> {
  const full = mergeMantineTheme(DEFAULT_THEME, theme);
  // The same merge MantineCssVariables makes, and the same precedence the
  // stylesheet gives it: a scheme's block outranks the plain :root block.
  const merged = deepMerge(defaultCssVariablesResolver(full), cssVariablesResolver(full));
  return { ...merged.variables, ...merged[scheme] };
}

const VARIABLE_REFERENCE = /^var\(\s*(--[\w-]+)\s*(?:,\s*(.+))?\)$/;
const RGB = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*(?:,\s*([\d.]+)\s*)?\)$/;
/** Deeper than any chain the theme writes; a cycle would otherwise never end. */
const MAX_REFERENCES = 16;

/** A colour value — hex, rgb(), or a chain of var() — as the hex it resolves to. */
export function resolveColour(value: string, variables: Record<string, string>): string {
  let current = value.trim();
  for (let step = 0; step < MAX_REFERENCES; step += 1) {
    const reference = VARIABLE_REFERENCE.exec(current);
    if (!reference) return asHex(current);
    const [, name, fallback] = reference;
    const next = variables[name] ?? fallback;
    if (next === undefined) throw new Error(`${name} is not defined in this scheme`);
    current = next.trim();
  }
  throw new Error(`${value} does not resolve within ${MAX_REFERENCES} references`);
}

function asHex(colour: string): string {
  if (/^#(?:[0-9a-f]{3}){1,2}$/i.test(colour)) return colour;
  const rgb = RGB.exec(colour);
  // A translucent colour's contrast depends on what is under it, which a
  // value alone does not say; refuse rather than measure the wrong pair.
  if (rgb && (rgb[4] === undefined || Number(rgb[4]) === 1)) {
    return `#${rgb.slice(1, 4).map((channel) => Number(channel).toString(16).padStart(2, '0')).join('')}`;
  }
  throw new Error(`cannot measure ${colour}: not an opaque hex or rgb colour`);
}

/**
 * The custom properties on the element of `rootClass` that draws `text`.
 *
 * Reads the nearest element carrying the class before the text, which is the
 * control's root: Mantine writes a control's colours there, as inline custom
 * properties, and the label inside it inherits them.
 */
export function customPropertiesOf(html: string, rootClass: string, text: string): Record<string, string> {
  const at = html.indexOf(`>${text}<`);
  if (at < 0) throw new Error(`nothing draws "${text}"`);
  const classAt = html.lastIndexOf(rootClass, at);
  if (classAt < 0) throw new Error(`"${text}" is not inside a ${rootClass}`);
  const tag = html.slice(html.lastIndexOf('<', classAt), html.indexOf('>', classAt) + 1);
  const style = /\sstyle="([^"]*)"/.exec(tag)?.[1] ?? '';
  const properties: Record<string, string> = {};
  for (const declaration of style.split(';')) {
    const colon = declaration.indexOf(':');
    if (colon > 0) properties[declaration.slice(0, colon).trim()] = declaration.slice(colon + 1).trim();
  }
  return properties;
}
