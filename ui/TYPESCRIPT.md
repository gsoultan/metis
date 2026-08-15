# TypeScript setup

This project compiles with **TypeScript 7.0.2** and lints with **TypeScript
6.0.3**, side by side. That is the arrangement Microsoft describes for TS 7, not
a workaround for this codebase.

## Why two

TypeScript 7 is the native (Go) compiler. Its package ships `tsc` and a new
`typescript/unstable/*` surface — and **not** the classic JS API:

```json
"exports": {
  ".": "./lib/version.cjs",
  "./unstable/sync": "./dist/api/sync/api.js",
  "./unstable/ast": "./dist/ast/index.js"
}
```

`import ts from 'typescript'` no longer gets you a compiler. Every tool built on
that API — typescript-eslint, and `ts-api-utils` underneath it — therefore needs
TypeScript 6 present. typescript-eslint says so itself rather than failing
obscurely:

```
typescript-eslint does not support TS 7.0.
Please see …/announcing-typescript-7-0/#running-side-by-side-with-typescript-6.0
```

Its peer range is `>=4.8.4 <6.1.0`, and support for TS 7 is tracked in
typescript-eslint#10940.

## How it is wired

- **`typescript@7.0.2`** provides `tsc`. `bun run typecheck` and `bun run build`
  use it, so the gate checks the code against the compiler that will compile it.
  The whole project typechecks in about half a second.
- **`typescript-for-eslint`** is `npm:typescript@6.0.3` under another name, so it
  does not claim `typescript` and confuse the compiler above.
- **`scripts/link-lint-typescript.mjs`** runs on `postinstall` and links that
  copy into the `node_modules` of every package in the lint chain that needs the
  JS API.

The link is needed because those packages do `require('typescript')`, which
cannot be pointed at an alias — a nested copy is the only thing they will pick
up. Package-scoped `overrides` express the same intent and bun does not apply
them here, which is why it is a script.

The script **discovers** its targets: anything under `node_modules` declaring a
`typescript` dependency or peer dependency. A hand-written list went stale
within one attempt — `ts-api-utils` is not a `@typescript-eslint/*` package and
needs the compiler just the same.

## When to collapse this

When typescript-eslint admits TS 7 (watch issue #10940, or check directly):

```bash
npm view typescript-eslint@latest peerDependencies.typescript
```

Then delete `typescript-for-eslint`, `scripts/link-lint-typescript.mjs` and the
`postinstall` script. Nothing else changes: the `typecheck` and `build` scripts
already run the real `tsc`.

## Note

`tsc --noEmit` against the root `tsconfig.json` checks **nothing** — it declares
`"files": []` and only project references. Always use `--build` / `-b`.
