# Hermod BPM — Claude Code project instructions

**Read [`AGENTS.md`](AGENTS.md) first, on every task, before opening any file.**

`AGENTS.md` is the always-on operating manual for this repository. It defines:

1. **What this system is** — a BPMN 2.0 orchestrator + DMN engine where process definitions
   are *untrusted user input* and every process instance is a *durable business commitment*.
   The non-negotiables in §0 apply to every change.
2. **The always-on loop** — Discover → Work → Verify → Persist (§1).
   `graphify-out/graph.json` exists: **query the graph before reading source.**
3. **The Developer Profile Panel** (§2) — 14 profiles with `Owns · Vetoes · Proof`.
   Adopt a **Driver**, re-read your diff as the **Challenger**, answer its vetoes in writing.
   §2.2 routes files → Driver + mandatory Challengers.
4. **The verification gate** (§4) — the exact commands, and the current red baseline.
5. **The task summary format** (§5) — every non-trivial task ends with it.

## Standing rules

- **Coding standards are in [`.junie/guidelines.md`](.junie/guidelines.md)** — normative for
  style, layering, folder structure, Go/React idioms and SQL. Read it before writing code.
  `AGENTS.md` governs *what must be proven*; `.junie/guidelines.md` governs *how to write it*.
- **Roadmap priority is fixed**: `P0 Security & Reliability` → `P1 Scalability & Performance`
  → `P2 UX Delight`. See [`.junie/roadmap.md`](.junie/roadmap.md).
- **`go run ./cmd/gobpm --build-ui` must run before `go build ./...`** on a fresh clone —
  `ui/embed.go` embeds `ui/dist`, which is gitignored.
- **`go test ./server/...` is not the test suite.** It skips the entire `tests/` tree.
  Use `make test` (or `go test ./...`). `make gate` runs the whole verification gate.
- **Never report done on an unrun command.** Paste the output.
- **Never add AI co-authorship trailers** to commits, PRs, tags, or comments.

## Every task, minimum bar

```
1. graphify query the area          →  don't re-derive structure by reading files
2. name Driver + Challenger         →  from AGENTS.md §2.2
3. make the change                  →  .junie/guidelines.md rules apply
4. re-read the diff as Challenger   →  answer every veto in writing
5. run the gate (AGENTS.md §4)      →  paste real output
6. rtk graphify update .            →  keep the graph in sync with the diff
```
