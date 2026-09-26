import { describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const SRC = join(import.meta.dir, '..');

/** Every component file under src, generated code and tests aside. */
function componentFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === 'gen' ? [] : componentFiles(path);
    return name.endsWith('.tsx') && !name.endsWith('.test.tsx') ? [path] : [];
  });
}

/** The names a file holds the store's expert-mode flag under. */
function flagNames(source: string): string[] {
  const names = new Set(['expertMode']);
  for (const match of source.matchAll(/const\s+(\w+)\s*=\s*useAppStore\(\s*\(\s*(\w+)\s*\)\s*=>\s*\2\.expertMode\s*\)/g)) {
    names.add(match[1]);
  }
  for (const match of source.matchAll(/const\s*\{([^}]*)\}\s*=\s*useAppStore\(\s*\)/g)) {
    for (const part of match[1].split(',')) {
      const [field, alias] = part.split(':').map((name) => name.trim());
      if (field === 'expertMode') names.add(alias || field);
    }
  }
  return [...names];
}

/** A JSX opening tag, skipping the `>` of any arrow inside its braces. */
function openingTag(source: string, start: number): string {
  let depth = 0;
  for (let index = start; index < source.length; index++) {
    const character = source[index];
    if (character === '{') depth++;
    if (character === '}') depth--;
    if (character === '>' && depth === 0) return source.slice(start, index + 1);
  }
  return source.slice(start);
}

/** The opening tag of each control whose `checked` is the flag, however it was read. */
function flagControls(source: string): string[] {
  const held = flagNames(source).join('|');
  const inline = String.raw`useAppStore\(\s*\(\s*(\w+)\s*\)\s*=>\s*\1\.expertMode\s*\)`;
  const bound = new RegExp(String.raw`checked=\{\s*(?:${held}|${inline})\s*\}`, 'g');
  return [...source.matchAll(bound)].map((match) => openingTag(source, source.lastIndexOf('<', match.index)));
}

/** What a screen reader calls the control: its aria-label, or else its label. */
function accessibleName(tag: string): string | undefined {
  return /aria-label="([^"]*)"/.exec(tag)?.[1] ?? /\slabel="([^"]*)"/.exec(tag)?.[1];
}

const SWITCHES = componentFiles(SRC).flatMap((file) =>
  flagControls(readFileSync(file, 'utf8')).map((tag) => [relative(SRC, file), tag] as const),
);

/**
 * Every switch for the flag has one name, wherever it appears.
 *
 * The decision editor's read "Expert", and a check that listed the switches it
 * knew of by file could not notice it: there were four, and it named three.
 * This one finds every control whose `checked` is the flag, in every
 * component.
 */
describe('the expert-mode switch', () => {
  it('is found everywhere it is', () => {
    // Guards the search itself: one that found nothing would pass below.
    expect(SWITCHES.map(([file]) => file)).toEqual(expect.arrayContaining([
      'components/shell/AppHeader.tsx',
      'pages/Settings.tsx',
      'components/PropertyPanel.tsx',
      'pages/DecisionEditor.tsx',
    ]));
  });

  it.each(SWITCHES)('is called Expert mode in %s', (_file, tag) => {
    expect(accessibleName(tag)).toBe('Expert mode');
  });
});
