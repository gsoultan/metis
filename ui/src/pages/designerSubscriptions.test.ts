import { describe, expect, it } from 'bun:test';
import { existsSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';

const SRC = join(import.meta.dir, '..');
const DESIGNER_ROUTE = join(SRC, 'routes', '_authenticated.designer.lazy.tsx');

/** What a module imports when it runs. A type-only import loads nothing. */
function runtimeImports(source: string): string[] {
  const statics = [...source.matchAll(/^\s*(?:import|export)\s+(?!type\s)[^'";]*?\sfrom\s+['"]([^'"]+)['"]/gm)];
  const bare = [...source.matchAll(/^\s*import\s+['"]([^'"]+)['"]/gm)];
  const dynamic = [...source.matchAll(/import\(\s*['"]([^'"]+)['"]\s*\)/g)];
  return [...statics, ...bare, ...dynamic].map((match) => match[1]);
}

/** The source file an import loads, when it is one of ours. */
function resolveImport(from: string, specifier: string): string | undefined {
  if (!specifier.startsWith('.')) return undefined;
  const base = resolve(dirname(from), specifier);
  return [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')]
    .find((candidate) => /\.tsx?$/.test(candidate) && existsSync(candidate) && statSync(candidate).isFile());
}

/** Every module of ours the designer route loads: its imports, theirs, and so on. */
function modulesLoadedBy(entry: string): string[] {
  const loaded = new Set<string>();
  const pending = [entry];
  while (pending.length > 0) {
    const file = pending.pop() as string;
    if (loaded.has(file)) continue;
    loaded.add(file);
    for (const specifier of runtimeImports(readFileSync(file, 'utf8'))) {
      const target = resolveImport(file, specifier);
      if (target !== undefined) pending.push(target);
    }
  }
  return [...loaded].map((file) => relative(SRC, file)).sort();
}

const DESIGNER_MODULES = modulesLoadedBy(DESIGNER_ROUTE);

/**
 * Nothing the designer loads subscribes to the whole app store.
 *
 * A component subscribed to the whole store re-renders on every write to any
 * field of it, and so does everything it renders. The palette's own hook for
 * the connector list, and the designer page and route above it, subscribed
 * that way, so collapsing the navigation re-rendered the palette, although it
 * reads nothing that changes. An earlier check looked only at the palette and
 * the canvas steps, where the subscription was not.
 */
describe('the designer', () => {
  it('loads the route, the palette, the canvas steps and the property panel', () => {
    // Guards the walk itself: a walk that found nothing would pass the check
    // below having checked nothing.
    expect(DESIGNER_MODULES).toEqual(expect.arrayContaining([
      'routes/_authenticated.designer.lazy.tsx',
      'pages/ProcessDesigner.tsx',
      'components/DesignerSidebar.tsx',
      'components/BPMNNodes.tsx',
      'components/PropertyPanel.tsx',
      'hooks/useConnectors.ts',
    ]));
  });

  it('subscribes to the store only through selectors, in every module it loads', () => {
    const wholeStore = DESIGNER_MODULES.filter((file) => /useAppStore\(\s*\)/.test(readFileSync(join(SRC, file), 'utf8')));

    expect(wholeStore).toEqual([]);
  });
});
