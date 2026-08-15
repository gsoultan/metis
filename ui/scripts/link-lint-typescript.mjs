/**
 * Give typescript-eslint the compiler it can actually load.
 *
 * The project typechecks with TypeScript 7, which is the point of installing
 * it. typescript-eslint cannot load TS 7 — its peer range is <6.1.0 and
 * typescript-estree throws at require time, taking the whole lint run with it:
 *
 *   TypeError: Cannot read properties of undefined (reading 'Cjs')
 *
 * It resolves the compiler with require('typescript'), so it cannot be pointed
 * at an alias. What it will pick up is a copy inside its own node_modules,
 * which is what this puts there. Package-scoped overrides would express the
 * same thing, and bun does not apply them for this case.
 *
 * Delete this, along with typescript-for-eslint, the moment typescript-eslint
 * admits TS 7. See TYPESCRIPT.md.
 */
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, symlinkSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const source = join(root, 'node_modules', 'typescript-for-eslint');

// Every package in the lint chain that reaches for the compiler itself,
// discovered rather than listed: anything declaring a typescript dependency or
// peer dependency needs the copy it can load, and the set changes with the
// dependency tree.
const consumers = discoverConsumers();

function discoverConsumers() {
  const modules = join(root, 'node_modules');
  const found = [];

  for (const dir of candidateDirs(modules)) {
    const manifest = join(dir, 'package.json');
    if (!existsSync(manifest)) continue;
    try {
      const pkg = JSON.parse(readFileSync(manifest, 'utf8'));
      const needs = { ...pkg.dependencies, ...pkg.peerDependencies }.typescript;
      // The compiler itself, and the copy for the linter, are not consumers.
      if (needs && pkg.name !== 'typescript' && !pkg.name.startsWith('typescript-for-eslint')) {
        found.push(dir);
      }
    } catch {
      // A half-written manifest is not this script's problem.
    }
  }
  return found;
}

function candidateDirs(modules) {
  const dirs = [];
  for (const entry of readdirSync(modules, { withFileTypes: true })) {
    if (!entry.isDirectory() && !entry.isSymbolicLink()) continue;
    if (entry.name.startsWith('@')) {
      const scope = join(modules, entry.name);
      for (const inner of readdirSync(scope, { withFileTypes: true })) {
        dirs.push(join(scope, inner.name));
      }
    } else {
      dirs.push(join(modules, entry.name));
    }
  }
  return dirs;
}

if (!existsSync(source)) {
  console.warn('[lint-typescript] typescript-for-eslint is not installed; skipping');
  process.exit(0);
}

const version = JSON.parse(readFileSync(join(source, 'package.json'), 'utf8')).version;
let linked = 0;

for (const base of consumers) {
  const target = join(base, 'node_modules', 'typescript');
  mkdirSync(dirname(target), { recursive: true });
  rmSync(target, { recursive: true, force: true });
  symlinkSync(relative(dirname(target), source), target, 'junction');
  linked += 1;
}

console.log(`[lint-typescript] linked TypeScript ${version} into ${linked} typescript-eslint packages`);
