# TypeScript setup

This project typechecks with **TypeScript 7** and lints with **TypeScript 5.9**,
on purpose.

## Why two

TypeScript 7 is the native (Go) port of the compiler. On this codebase:

| | |
| :-- | :-- |
| `tsgo --build` (TS 7, native) | **~0.4s** |
| `tsc -b` (TS 5.9, JS) | **~6.5s** |

That is a 16× difference on every typecheck, in the editor and in CI.

`typescript-eslint` cannot use it yet. Its peer range is
`typescript >=4.8.4 <6.1.0`, and with TS 7 installed as `typescript` it does not
degrade gracefully — `@typescript-eslint/typescript-estree` throws at require
time, taking the whole lint run with it.

## How it is wired

- `@typescript/native-preview` provides `tsgo`, used by `bun run typecheck` and
  `bun run build`. This is the real typecheck gate.
- `typescript@~5.9` stays installed so `typescript-eslint` resolves a compiler
  it supports. Nothing else uses it.

## When to collapse this

When `typescript-eslint` publishes a release whose peer range admits TS 7:

```bash
npm view typescript-eslint@latest peerDependencies.typescript
```

Then drop `typescript@5.9` and `@typescript/native-preview`, and install
`typescript@7` as the single dependency.

## Note

`tsc --noEmit` against the root `tsconfig.json` checks **nothing** — it declares
`"files": []` and only project references. Always use `--build` / `-b`.
