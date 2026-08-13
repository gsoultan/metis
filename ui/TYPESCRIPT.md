# TypeScript setup

This project typechecks with **TypeScript 7** and lints with **TypeScript 6**,
on purpose.

## Why two

TypeScript 7 is the native (Go) port of the compiler. On this codebase:

| | |
| :-- | :-- |
| `tsgo --build` (TS 7, native) | **~0.4s** |
| `tsc -b` (TS 5.9, JS) | **~6.5s** |

Measured when the split was introduced. The JS compiler is no longer installed
to typecheck with, so the number is kept as the reason rather than as a claim
about today.

That is a 16× difference on every typecheck, in the editor and in CI.

`typescript-eslint` cannot use it yet. Its peer range is
`typescript >=4.8.4 <6.1.0`, and with TS 7 installed as `typescript` it does not
degrade gracefully — `@typescript-eslint/typescript-estree` throws at require
time, taking the whole lint run with it:

```
TypeError: Cannot read properties of undefined (reading 'Cjs')
  at .../typescript-estree/dist/create-program/shared.js
```

Retested against the released `typescript@7.0.2` with `typescript-eslint@8.67.0`,
the newest of both: unchanged. An alias does not help either — typescript-estree
does `require('typescript')`, so whatever is installed under that name is what it
gets.

## How it is wired

- `@typescript/native-preview` provides `tsgo`, used by `bun run typecheck` and
  `bun run build`. This is the real typecheck gate.
- `typescript@6.0` stays installed so `typescript-eslint` resolves a compiler it
  supports. Nothing else uses it, and it is kept as close to 7 as the peer range
  allows so the rules see something near the semantics the gate checks against.

`typescript@7.0.2` is released and its `tsc` is the native compiler, so the
preview package will not be needed forever — but installing it as `typescript`
is what breaks lint, which is the whole reason for the split. The preview
package exists precisely so the native compiler can be run without claiming the
`typescript` name.

## When to collapse this

When `typescript-eslint` publishes a release whose peer range admits TS 7:

```bash
npm view typescript-eslint@latest peerDependencies.typescript
```

Then drop `typescript@6` and `@typescript/native-preview`, install `typescript@7`
alone, and change the `typecheck` and `build` scripts from `tsgo` to `tsc` —
`tsc --build --force` from typescript@7 typechecks this project in about the
same time as `tsgo`, both being the same compiler.

## Note

`tsc --noEmit` against the root `tsconfig.json` checks **nothing** — it declares
`"files": []` and only project references. Always use `--build` / `-b`.
