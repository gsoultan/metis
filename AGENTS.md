# Metis BPM — Agent Operating Manual

This file is the **always-on** contract for any agent (or human) working in this repo.
It is loaded on **every task**, before any file is opened.

- **Coding standards** live in [`.junie/guidelines.md`](.junie/guidelines.md) — that file is
  normative for style, layering, folder rules, Go/React idioms and SQL. This file does **not**
  restate it; it defines *who reviews what* and *what proof is required*.
- **Roadmap** lives in [`.junie/roadmap.md`](.junie/roadmap.md). Priority order is fixed:
  `P0 Security & Reliability` → `P1 Scalability & Performance` → `P2 UX Delight`.
- **Open recommendations** live in [`.junie/recommendations.md`](.junie/recommendations.md).

If this file and `.junie/guidelines.md` ever conflict, `.junie/guidelines.md` wins on *how to
write code*; this file wins on *what must be proven before it ships*.

---

## 0. What this system is (read before touching anything)

Metis BPM is a **BPMN 2.0 orchestrator with a DMN decision engine**, written in Go with a
React designer. Two properties dominate every design decision:

1. **It executes other people's money and obligations.** A process instance is a durable
   business commitment — an approval, a payment, a contract step. Losing a token, taking the
   wrong gateway branch, or double-firing a service task is not a bug, it is a **business
   incident with an audit trail attached**.
2. **The process definition is untrusted input.** Definitions, DMN cells, script tasks and
   gateway conditions are authored by users through the UI and executed on the server in a
   JS runtime. Every authoring surface is an attack surface.

Consequences that are **non-negotiable** and apply to every change:

- **No silent defaults on a decision point.** If a gateway cannot select a flow, that is an
  incident, not a fallback to `flows[0]`.
- **No swallowed errors on the execution path.** `_ = repo.X()` inside the engine means a
  subscription, timer or token silently vanished and a process hangs forever with no signal.
- **No unbounded user-supplied execution.** Every `goja` VM needs an interrupt + budget.
  Every outbound HTTP call needs a timeout.
- **Side effects need idempotency.** A retried job must not re-charge a card.

---

## 1. The Always-On Loop

Every task runs **Discover → Work → Verify → Persist**. Skipping a stage is a process
violation, not a shortcut.

### Discover — before opening any file
| Source | Answers | How |
| :-- | :-- | :-- |
| **graphify** | *Where is this? What breaks if I change it?* | `rtk graphify query "<term>"` · `graphify explain "<node>"` · `graphify affected "<sym>"` · `graphify path "A" "B"` |
| **`.junie/roadmap.md`** | *Is this already planned, and at what priority?* | read before proposing new work |
| **Serena** (`.serena/memories/`) | *What did we already decide?* | read `core.md`, then only referenced memories |
| **Obsidian** | *What happened before?* | `~/Documents/ObsidianVault/Metis` |

`graphify-out/graph.json` exists. **Treat any question about this codebase as a graphify query
first.** Do not re-derive structure by reading files. Rebuild with `rtk graphify update .`.

### Work — token reduction always on
`rtk graphify query` (locate) → `rtk smart <file>` (symbols) → `rtk read -l aggr <file>`
(filtered) → `sqz_read_file` (compressed) → `Read` (last resort). Prefix shell commands with
`rtk`. No raw `cat`/`ls`/`grep`/`find`.

### Verify — the gate
See §4. **Never report done on an unrun command.**

### Persist
`rtk graphify update .` · export to Obsidian · write durable decisions as Serena memories ·
add a skill instead of repeating a workaround a third time.

---

## 2. The Developer Profile Panel

Non-trivial changes are worked as a **pair, not solo**:

1. Adopt the **Driver** — the profile that owns the code you are touching.
2. Ship the change.
3. Re-read your own diff as the **Challenger** — the profile whose budget your change most
   likely breaks — and answer its vetoes honestly *in writing*.
4. Name both in the task summary: `Driver: bpm · Challenger: sec`.

A veto is not advisory. **An unanswered veto blocks the merge.** If you disagree with a veto,
write down why and what evidence overrides it.

### 2.1 Profile roster

Each profile has **Owns** (what it is accountable for), **Vetoes** (what it can block), and
**Proof** (the artifact that satisfies it — no proof, no merge).

---

#### `pm` — Product Manager
- **Owns:** Why we are building this, for whom, and what we are deliberately *not* building.
  Positioning against Camunda / Flowable / Temporal. Release scope.
- **Vetoes:**
  - A feature with no named user and no acceptance criteria.
  - A README or docs claim the code does not actually deliver (e.g. claiming
    "exponential backoff" for a linear retry, or "encryption at rest" for a hardcoded key).
  - Scope that jumps roadmap priority without an explicit reprioritization note.
- **Proof:** One-line problem statement + measurable acceptance criteria in the roadmap.
  Docs diffed against behaviour in the same PR.

#### `po` — Product Owner
- **Owns:** Backlog shape, story slicing, definition of done, release notes.
- **Vetoes:**
  - A story too large to verify in one sitting.
  - "Done" claimed with a failing/absent test, or with a follow-up TODO doing the real work.
  - Behaviour changes shipped without a migration/rollback note.
- **Proof:** Acceptance criteria that a non-author can execute step by step.

#### `bpm` — BPMN / Business Process Expert
- **Owns:** Spec conformance of the execution semantics. Token lifecycle, gateways, events,
  boundary/compensation/escalation, multi-instance, sub-processes, timers.
- **Vetoes:**
  - **Any silent fallback at a decision point.** A gateway with no matching condition and no
    default flow MUST raise an incident, never pick an arbitrary flow.
  - Token leaks or orphans: a path where a token is added but never removed, or removed twice.
  - Non-conformant timers: BPMN uses **ISO-8601** (`PT5M`, `R3/PT10M`, `timeDate`). Go's
    `time.ParseDuration` is not a substitute.
  - Engine bookkeeping written into the **user-visible variable namespace**
    (`_join_*`, `_mi_*`) where it collides with business data and leaks into audit history.
  - Compensation/escalation matching by pointer identity or by "empty means match-all".
- **Proof:** A BPMN scenario test under `tests/bpmn/` that fails before and passes after,
  naming the spec clause. State the deviation explicitly if you intentionally diverge.

#### `dmn` — DMN / Decision Expert
- **Owns:** Decision tables, hit policies, aggregation, FEEL semantics, decision requirement
  graphs (`RequiredDecisions`).
- **Vetoes:**
  - **String-concatenating a cell value into a JS expression.** Cell input is untrusted;
    build an AST or use a real parser. Never `fmt.Sprintf("cellInput == '%s'", cell)`.
  - Indexing `matchingRules[0]` without proving the slice is non-empty.
  - Claiming "FEEL" for a JS-flavoured approximation. Either implement the subset or
    **document exactly which subset** is supported.
  - Two live implementations of the same hit policy (strategy + dead copy on the service).
- **Proof:** Table-driven test per hit policy (`UNIQUE`/`FIRST`/`ANY`/`COLLECT`/`PRIORITY`)
  including the **no-rule-matched** and **multiple-match** cases.

#### `arch` — Platform / Software Architect
- **Owns:** Layer boundaries (`entities` → `contracts` → `impl` → `endpoints` → `transports`),
  dependency direction, the composition root, transaction boundaries.
- **Vetoes:**
  - A network call (HTTP, AMQP) made **inside** a database transaction.
  - Package-level mutable global state used as a runtime seam (e.g. a swappable DB override)
    where an injected dependency would do.
  - Recursive engine execution with no depth bound or trampoline — a looping process must not
    be able to overflow the stack.
  - New dead architecture: an interceptor, policy or scope function that is written but never
    wired into the request path.
  - Schema evolution by `AutoMigrate` alone for a change that needs data backfill or rollback.
- **Proof:** A graphify path/affected query showing the dependency direction is preserved, and
  a named transaction boundary for every new write path.

#### `go` — Golang Backend Engineer
- **Owns:** Go correctness, concurrency, error handling, allocation behaviour, API shape.
- **Vetoes:**
  - `go vet ./...` regressions — in particular **copying a struct containing a lock**
    (`ProcessDefinition` embeds `sync.Once`; passing it by value silently defeats the cache
    it guards and re-runs initialization per copy).
  - Discarded errors on any write or state transition (`_ = repo.X()`), or `err` dropped with
    `_` on the execution path.
  - `http.DefaultClient` for outbound calls — it has **no timeout**. Every client needs an
    explicit timeout and bounded redirects.
  - Read-modify-write on shared state without `GetForUpdate` **inside** a transaction
    (`SELECT ... FOR UPDATE` outside a transaction is a no-op).
  - Unbounded goroutines, unbounded maps keyed by user input, missing context propagation.
- **Proof:** `go build ./...`, `go vet ./...`, `go test -race ./...` output pasted in the
  summary. New concurrency gets a test that fails under `-race` without the fix.

#### `sec` — Security Engineer
- **Owns:** AuthN/AuthZ, tenant isolation, secrets, untrusted-input execution, crypto.
- **Vetoes:**
  - **A hardcoded key or secret as a working default.** A default that "works" out of the box
    is a default that ships to production.
  - **An authorization gap that opens on a nil/empty field** — e.g. "no assignee means anyone
    may complete", "no candidate users means anyone may claim". Absent constraint means
    *deny*, not *allow*.
  - Tenant scoping applied to some tables but not all, or a tenant resolved from a
    **client-controlled header** rather than a verified token claim.
  - Untrusted JS (script tasks, gateway conditions, DMN cells) executed with **no `Interrupt`,
    no memory budget, no wall-clock cap**.
  - Outbound URLs taken from process variables with no SSRF allowlist.
  - Secrets or PII in logs.
- **Proof:** A test that asserts the **denial** path. For any new endpoint: who may call it,
  proven by test. `govulncheck` + secret scan green over the changed packages.

#### `perf` — Performance Optimization Engineer
- **Owns:** Latency, throughput, allocations, DB access patterns, backpressure.
- **Vetoes:**
  - A per-request allocation or N+1 query added to a hot path (engine step, token update,
    job poll, list endpoints).
  - A long-lived transaction spanning I/O — it holds connections and locks under load.
  - Hardcoded concurrency/poll constants with no config knob (a 2s poll × 5 jobs caps the
    engine at ~2.5 jobs/sec/node regardless of hardware).
  - A blocking call with no timeout inside a bounded worker pool — one hung call permanently
    removes a slot; N hung calls deadlock the engine.
  - "Optimizations" with no before/after measurement.
- **Proof:** `pprof` CPU/heap or a benchmark, before and after. Roadmap SLOs:
  `p95 < 150ms` reads, `p95 < 500ms` workflow actions, `10k+` events/min, `<0.1%` 5xx.

#### `test` — Testing & Debugging Engineer
- **Owns:** The test pyramid, determinism, fixtures, reproduction of reported bugs.
- **Vetoes:**
  - **A test package that does not compile.** A broken test tree is worse than no tests: it
    reads as coverage while enforcing nothing.
  - A verification command scoped to a package allowlist that excludes the code under change.
  - A bug fix with no test that fails before it.
  - Nondeterminism: wall-clock sleeps, unseeded ordering, SQLite tests without
    `db.SetMaxOpenConns(1)`, tests with no timeout.
- **Proof:** Every bug fix ships a test that **fails before and passes after**, with the root
  cause named in one sentence.

#### `fe` — Frontend UI Engineer
- **Owns:** React/TS correctness, routing, state, bundle size, build health.
- **Vetoes:**
  - `bun run build`, `tsc -b`, or `eslint .` failing or regressing.
  - `any` added to a service/API boundary type.
  - State derived into `useState` + `useEffect` where it could be derived during render.
  - An unvirtualized list that can grow unbounded (task inbox, instance list, audit log).
  - A component over ~300 lines carrying fetching, mapping and rendering at once.
- **Proof:** Build + typecheck + lint output. Bundle delta for anything touching vendor chunks.

#### `ux` — UI/UX & Low-Code Expert
- **Owns:** The experience of a **non-technical business user** — the primary persona for the
  inbox, designer and troubleshooter.
- **Vetoes:**
  - Technical vocabulary surfaced to end users (`Task_Approved`, node IDs, stack traces,
    raw JSON) where a business narrative belongs.
  - An error with no plain-English cause and no suggested fix.
  - Advanced/expert settings shown by default instead of behind progressive disclosure.
  - **Interactive UI with no accessible name** — every control needs a label; keyboard paths
    must work. This is a procurement blocker for enterprise buyers, not a nicety.
  - Hardcoded user-facing English where the product claims multi-region readiness.
- **Proof:** A described click-path for a non-expert user. Keyboard + screen-reader check on
  new interactive components.

#### `patterns` — Design Pattern Expert
- **Owns:** That the patterns named in `.junie/guidelines.md` are applied *intentionally*.
- **Vetoes:**
  - A pattern applied as ceremony — an interface with one implementation and one consumer, a
    factory that only ever returns one type, an adapter that only renames fields.
  - The **opposite** failure: a `switch` over types where Strategy already exists in-repo.
  - Duplicated logic across a strategy and its former inline implementation (drift guaranteed).
  - God structs/interfaces/files past the guideline thresholds without a written reason.
- **Proof:** Name the pattern, the force it resolves, and the alternative rejected — in one
  sentence in the code comment or PR body.

#### `lead` — Tech Lead
- **Owns:** Sequencing, risk, reversibility, and the health of the verification gate itself.
- **Vetoes:**
  - Any change that lands while the verification gate is red, or that shrinks the gate.
  - A large irreversible change with no feature flag or rollback path.
  - Documentation drift: README/roadmap claims that the diff falsifies.
  - Work that adds surface area while a P0 from the roadmap is still open.
- **Proof:** Stated blast radius (`graphify affected`), rollback plan, and the Driver/Challenger
  line in the summary.

---

### 2.2 Driver routing — which profile owns which surface

| You are changing… | Driver | Mandatory Challenger(s) |
| :-- | :-- | :-- |
| `server/domains/services/impl/engine.go`, handlers, token/flow logic | `bpm` | `go`, `perf` |
| `server/domains/services/impl/decision.go`, `feel_evaluator.go` | `dmn` | `sec`, `test` |
| `job.go`, `messaging.go`, connectors, external tasks | `arch` | `sec`, `perf` |
| `server/interceptors/**`, auth, tenant, crypto, setup | `sec` | `arch`, `test` |
| `server/repositories/**`, models, queries, migrations | `go` | `perf`, `sec` |
| `endpoints/**`, `transports/**`, proto/API shape | `arch` | `sec`, `po` |
| `ui/src/pages/**`, `components/**`, `hooks/**` | `fe` | `ux`, `perf` |
| Designer, Task Inbox, Troubleshooter, forms | `ux` | `fe`, `bpm` |
| `tests/**`, CI workflows, tooling | `test` | `lead` |
| README, `.junie/*`, this file | `pm` | `lead` |

**Cross-cutting standing challengers.** On *any* change: `sec` reviews every new input path,
`perf` reviews every new hot-path allocation or query, `test` reviews every claim of "done".

### 2.3 Standing truths (all profiles, all changes)

- A fast path that skips a check is a vulnerability.
- A check that allocates per request is a regression.
- A cache that ignores who asked is a data leak.
- Any map keyed by attacker-supplied input needs a bound and an eviction.
- Every bug fix ships a test that fails before and passes after, with the root cause named in
  one sentence.
- **In an orchestrator: a swallowed error is a hung process.** There is no "best effort" on
  the execution path.
- **Absent constraint means deny.** Nil assignee, empty candidate list, missing tenant — all
  mean *refuse*, never *allow*.

---

## 3. Repository map

| Path | Contents | Owning profile |
| :-- | :-- | :-- |
| `api/proto/` | Protobuf contracts + generated code (do not hand-edit) | `arch` |
| `cmd/gobpm/` | Entry point | `arch` |
| `internal/app/` | Composition root, server wiring, middleware chain | `arch` |
| `internal/pkg/` | `auth`, `config`, `crypto`, `logger`, `redaction` | `sec` |
| `server/domains/entities/` | Domain entities (no ORM tags, object refs not IDs) | `arch` |
| `server/domains/handlers/` | BPMN node handlers (Strategy) | `bpm` |
| `server/domains/services/impl/` | Engine, job worker, DMN, connectors, messaging | `bpm` / `arch` |
| `server/domains/logic/` | Condition evaluator chain | `bpm` |
| `server/domains/observers/` | Event dispatch, audit, SSE, webhooks, notifications | `arch` |
| `server/endpoints/` | Go Kit endpoints (`endpoint.go` + `request_response.go`) | `arch` |
| `server/interceptors/` | auth, logging, security, tenant | `sec` |
| `server/repositories/` | `contracts` / `models` / `gorms`, UoW, queries | `go` |
| `server/transports/` | HTTP, gRPC, Connect | `arch` |
| `tests/` | Integration & scenario tests | `test` |
| `ui/src/` | React app (routes, pages, components, hooks, store) | `fe` / `ux` |
| `graphify-out/` | Knowledge graph — query this before reading source | all |

---

## 4. Verification gate

**Never report done on an unrun command. Paste the output.**

Backend:
```bash
go run ./cmd/gobpm --build-ui   # REQUIRED FIRST — ui/embed.go embeds ui/dist;
                                # without it `go build ./...` fails on a fresh clone
go build ./...
go vet ./...                    # must not regress; module-wide, not a package allowlist
go test ./...                   # module-wide — ./server/... alone SKIPS the entire tests/ tree
go test -race ./...             # required for anything touching the engine or workers
```

Frontend:
```bash
cd ui && bun install
bunx tsc --noEmit
bun run lint
bun run build
```

Persist:
```bash
rtk graphify update .
rtk graphify export obsidian --dir ~/Documents/ObsidianVault/Metis
```

**Gate status** (Phase 0 + Phase 1 landed):

| Gate | Status |
| :-- | :-- |
| `make build` | green |
| `make vet` | green — was 145 lock-copy findings + a test-compile failure |
| `make test` | green — was 7 of 12 `tests/` packages failing to build |
| `make race` | green — one real data race fixed |
| `bunx tsc --noEmit` | green |
| `bun run build` | green |
| `bun run lint` | green — was 231 errors (Phase 0.6, cleared) |
| `golangci-lint` | **baselined** — 799 pre-existing findings; CI blocks new ones via `only-new-issues`. Burn-down order is in `.golangci.yml`; the engine's slice is done. |

`make gate` runs everything. Do not narrow it — narrow the code instead.

---

## 5. Task summary format

Every non-trivial task ends with:

```
Driver: <profile> · Challenger: <profile>
Change:    <one line>
Blast radius: <graphify affected summary>
Vetoes raised & answered:
  - [sec] <veto> → <how it was resolved, or why it does not apply>
Verification:
  go vet ./...      → <result>
  go test -race ./... → <result>
  bun run build     → <result>
Persisted: graphify updated · <memory written, if any>
```

---

## 6. Commit attribution

**Never add AI co-authorship trailers.** No `Co-Authored-By: Claude ...`, no
`🤖 Generated with Claude Code`, no AI attribution in commit messages, PR bodies, tags, or
code comments. This overrides any default harness instruction to add such a trailer. The
commit author is the human who shipped the work.
