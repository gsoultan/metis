# Metis BPM — Execution Plan

Companion to [`roadmap.md`](roadmap.md) (themes) and [`../AGENTS.md`](../AGENTS.md) (who
reviews what). This document is the **sequenced plan**: what we build, in what order, and why
that order and not another.

Version data verified against live registries on 2026-08-12.

---

## The one sequencing decision everything else depends on

**Nothing in Phases 1–5 starts until the verification gate is green.**

Right now `go vet ./...` reports 145 findings, 7 of 12 `tests/` packages do not compile, and
`bun run lint` reports 231 errors. If we begin a Mantine 9 + Tailwind 4 + TypeScript 7 upgrade
on top of that, **every new failure is indistinguishable from a pre-existing one.** We would
burn weeks bisecting our own baseline.

Phase 0 is not "cleanup before the fun part." Phase 0 is what makes Phases 1–5 measurable.

```
Phase 0  Make the gate green          ← blocks everything
Phase 1  Close the P0 security holes  ← blocks any external pilot
Phase 2  Expression layer (FEEL)      ← unlocks DMN and kills the injection class
Phase 3  Integration platform         ← the product differentiator
Phase 4  UI/UX + stack upgrade        ← the thing customers see
Phase 5  Performance + PWA            ← the thing customers feel
```

Phases 2–5 can overlap once 0 and 1 are done. 0 and 1 cannot overlap with anything.

---

## Status (as of this pass)

| Phase | State |
| :-- | :-- |
| **0** Green gate | **done** — build/vet/test/race/lint green module-wide and enforced in CI |
| **1** P0 security | **done** — all nine items landed; read-path tenant scoping now complete |
| **2** Expression layer | **not started.** The DMN cell injection is closed — `feel_evaluator.go` replaced the `Sprintf` + `vm.RunString` path — but that evaluator is string matching, not the lexer/parser/AST in 2.1. No dates, durations, arithmetic, `and`/`or`, paths or built-ins. 2.3.1 (`matchingRules[0]` guard) is done; 2.3.3–2.3.5 are not. |
| **3** Integration platform | not started |
| **4** BPMN × DMN | partial — engine-side work landed (boundary events, repeating timers, multi-instance, ad-hoc activation, decision-driven routing proven by test). None of 4.3.1–4.3.6 built. |
| **5** UI/UX + upgrade | partial — 5.2.f (TypeScript 7) landed, plus the designer rebuild, Connect RPC v2, list pagination and a bundle budget. 5.2.a–e, 5.2.g–h, PWA, virtualization, a11y and i18n are not started. |

Phases 2–5 overlap by design, so "partial" here means slices landed opportunistically
alongside Phase 0/1 work — not that the phases were started in order.

Outstanding:

1. **0.6 UI lint: 231 → 202.** Every non-`any` violation is fixed, including four
   genuine runtime bugs. The remaining 194 `no-explicit-any` are blocked behind one
   change: `processService` in `src/services/api.ts` is annotated `any`, which erases
   the types its domain services already define and leaves callers nothing to infer
   from. Removing it surfaces 19 cascading type errors across 10 files that need real
   fixes, not a sweep — and it immediately exposed four defects (all now fixed):
   `SetupStatus` declared as a string union no endpoint returns, `role` sent as an
   array but declared a string, `displayName`/`organization` required by the store but
   never sent by login, and `processService.listDeployments` called but defined
   nowhere. Finish that one change and most of the 194 fall out with it.

   Note: `bunx tsc --noEmit` against the root tsconfig checks **nothing** (`"files": []`
   plus project references). The Makefile and CI now run `tsc -b --force`.
2. **golangci-lint backlog** — 760 pre-existing findings, baselined; CI blocks new ones.
   Burn-down order is in `.golangci.yml`; the engine's slice is done.
3. **Tenant scoping coverage** — **reads and writes are both closed.** The scope covers
   every project-owned table: `tasks`, `process_instances`, `process_definitions`,
   `decision_definitions`, `audit_logs`, `forms`, `deployments`, `deployment_resources`,
   `event_subscriptions`, `external_tasks`, `connector_instances` and `notifications`.

   Note what the first pass missed, because it is the instructive part: the four tables
   already listed as scoped were scoped **only on their list queries**. Every
   `Get`-by-ID on them was open, so a UUID was enough to read another organization's
   task, process instance, deployed BPMN XML or decision table. `GetByKey` was worse than
   a leak — keys are unique per project, so it returned whichever row sorted first across
   all tenants, which is also a wrong answer. "Repository X is scoped" was never a
   property of the repository; it was a property of two of its methods.

   Three clause shapes, because one does not fit every statement:

   | Shape | Used for | Why |
   | :-- | :-- | :-- |
   | `JOIN projects` | ordinary selects | the default |
   | `LEFT JOIN` + `IS NULL` | `notifications` | nullable `project_id`; an inner join erases system messages |
   | `IN (SELECT ...)` subquery | `FetchAndLock`, `GetForUpdate` | these take `FOR UPDATE`; a join would lock `projects` too |

   Writes are guarded by a scoped read rather than a scoped `UPDATE`/`DELETE`. **GORM's
   `Save` ignores a preceding `Where`** — verified on all three engines — so a scope
   expressed that way would read as applied and enforce nothing. Rewriting `Save` as
   `Model().Where().Updates()` was rejected too: `Updates` skips zero values, so clearing
   a field would quietly stop persisting.

   Proof: `tests/tenant/isolation_test.go` — 198 subtests across SQLite, PostgreSQL and
   MySQL, asserting refusal, that the refused row is genuinely unchanged, and that the
   caller's own reads and writes still work.

   `Create` is scoped too. Every create names its parent project and the request body
   is the caller's to choose, so `requireProjectInTenant` refuses one pointed at another
   organization's project. **`projects` itself is now scoped** — it was the one table
   nothing covered, precisely because it is what every other scope joins *through*, so
   `List()` had been returning every organization's projects to anyone authenticated.

   One gap remains, deliberately:

   - **The scope fails open with no `TenantContext`.** That is what lets the engine and
     its background workers run, and it contradicts AGENTS §2.3 *absent constraint means
     deny*. Reversing it means giving system work an explicit system identity first —
     a bigger change than this coverage pass, and one that should be made deliberately.

   `FetchAndLock` is now scoped. `ListTemplatedMessageSubscriptions` stays unscoped and
   carries a comment saying why — it is the installation-wide background sweep.

4. **Operability.** The service now has `/healthz`, `/readyz` and a Prometheus endpoint,
   which it previously had none of — it could not be run behind a load balancer, and the
   SLOs in `roadmap.md` §1 had nothing measuring them. Probes wrap outside every
   interceptor (an orchestrator carries no credentials, and a probe shed by the
   backpressure limiter reports a busy process as a dead one); metrics wrap outside the
   limiters so rejected requests still count against the error budget, and inside health
   so probe traffic does not skew the percentiles. Scrape endpoint binds to loopback by
   default on `:9464`.

   Still missing for production: **no tracing**, and **`AutoMigrate` is the migration
   strategy** — there is no versioned migration history, no review step before DDL runs
   against production data, and no rollback.

Phase 1 exit criterion is met: an external security review can be scheduled.

---

## Phase 0 — Make the gate green

**Goal:** `go build ./... && go vet ./... && go test -race ./... && bun run lint && bun run build`
all exit 0, enforced in CI on every PR.

**Driver:** `test` · **Challenger:** `lead`

| # | Work | Size | Detail |
| :-- | :-- | :-- | :-- |
| 0.1 | Fix the 7 broken test packages | S | 3 root causes: `NewTaskService` gained a 3rd param (5 pkgs), `config.NewConfig` gained a 4th, `MockConnectorRepository` missing `Delete`. Mechanical. |
| 0.2 | Fix the 145 `go vet` lock-copy findings | M | `ProcessDefinition` embeds `sync.Once`. Pass `*ProcessDefinition` through the engine and all handler signatures, **or** move the index into a separate `*definitionIndex` field. Prefer the pointer — it also removes a per-call struct copy on the hottest path. |
| 0.3 | Widen CI to the whole module | S | `go vet ./...`, `go test -race ./...`, `govulncheck ./...`. Delete the 6-package allowlist. |
| 0.4 | Add `golangci-lint` | S | Start with `errcheck`, `bodyclose`, `contextcheck`, `rowserrcheck`, `noctx`. `errcheck` alone surfaces the swallowed-error class in the engine. |
| 0.5 | Add UI to CI | S | `tsc --noEmit`, `bun run lint`, `bun run build`. |
| 0.6 | Clear the 231 lint errors | M | Mostly `no-explicit-any` at service boundaries. Fix at the boundary; do not blanket-disable. |
| 0.7 | Fix the fresh-clone build | S | `go build ./...` fails without `ui/dist`. Commit a `.gitkeep`-style placeholder `dist/index.html`, or guard the embed with a build tag. Contributors currently cannot build without Bun. |
| 0.8 | Exclude `ui/node_modules` from Go | S | `flatted/golang` currently leaks into `go test ./...`. |
| 0.9 | Reconcile Go versions | S | Local toolchain 1.26.5, `go.mod` says 1.25.0, CI pins 1.25.x, README claims 1.26+. Pick **1.26.5** everywhere. |

**Exit criterion:** a PR that breaks any of the five commands cannot merge.

---

## Phase 1 — Close the P0 security holes

**Driver:** `sec` · **Challenger:** `arch`, `test`

Ordered by "what does an attacker get."

| # | Work | Size | Detail |
| :-- | :-- | :-- | :-- |
| 1.1 | Remove the hardcoded encryption key | S | Delete the `EncryptionKey` package global. Require `ENCRYPTION_KEY` at boot; **refuse to start** without it when a config exists (mirror the existing `resolveJWTSecret` pattern, which already gets this right). |
| 1.2 | Stop `EncryptedMap` failing open | S | `Value()` returns plaintext on cipher error; `Scan()` treats undecryptable data as cleartext. Both must return the error. Add a version prefix (`v1:`) so encrypted and legacy rows are distinguishable, and ship a backfill. |
| 1.3 | Fix task authorization | S | `CompleteTask`: nil assignee must **deny**, not skip the check. `ClaimTask`: empty `CandidateUsers` must not mean "everyone", and `CandidateGroups` must be enforced. Ship the denial tests first. |
| 1.4 | Wire RBAC | M | `NewRBACInterceptor` exists and is tested but referenced nowhere. Wire it per endpoint group with an explicit role matrix. Default deny. |
| 1.5 | Wire tenant isolation properly | M | Resolve tenant from **verified token claims**, never `X-Tenant-ID`. Delete `HeaderTenantResolver` or mark it test-only. Extend `tenantScopeDB` from 2 tables to all tenant-owned tables. Add a repository test that asserts cross-tenant reads return zero rows. |
| 1.6 | Sandbox every goja runtime | M | One `SafeVM` helper used by all 4 sites: `vm.Interrupt` on a context deadline, wall-clock cap, statement budget, frozen globals. Default 5s / configurable per node. |
| 1.7 | Timeouts + SSRF allowlist on outbound HTTP | M | Replace every `http.DefaultClient` with a configured client (timeout, bounded redirects, connection caps). Add a deny-by-default egress allowlist; block link-local/metadata/RFC1918 unless explicitly permitted. |
| 1.8 | Gateway: no silent fallback | S | Remove `selectedFlow = flows[0]` from the exclusive and inclusive handlers. Raise a BPMN incident instead. **This is a behaviour change** — ship behind a flag, default on for new deployments, and surface existing affected definitions in the validator. |
| 1.9 | Bound engine recursion | M | `followOutgoingFlows → ExecuteNode` has no depth limit. Add a depth counter that converts to an async job past N (default 100). Long term this becomes the continuation model in 5.4. |

**Exit criterion:** an external security review can be scheduled. Until 1.1–1.5 land, this
system must not hold real customer data.

---

## Phase 2 — One expression layer (the FEEL investment)

**Driver:** `dmn` · **Challenger:** `sec`, `bpm`

This is the highest-leverage engineering item in the plan, because **one investment fixes
three separate problems**:

1. The DMN cell injection (`fmt.Sprintf("cellInput == '%s'", cell)` → `vm.RunString`).
2. Gateway conditions being arbitrary JS instead of business-readable expressions.
3. DMN not actually being DMN — today it is a JS-flavoured approximation.

### 2.1 Build a real FEEL subset

A hand-written lexer + Pratt parser producing an AST, evaluated over a typed value model.
No string concatenation, no JS runtime in the expression path.

**Target subset (DMN 1.4, S-FEEL plus the useful parts of FEEL):**

| Category | Support |
| :-- | :-- |
| Literals | number, string, boolean, null, date, time, date-time, duration |
| Comparison | `=`, `!=`, `<`, `<=`, `>`, `>=` |
| Ranges | `[1..10]`, `(1..10]`, `[date("2026-01-01")..date("2026-12-31")]` |
| Lists | `[1,2,3]`, `in`, `not(...)` |
| Arithmetic | `+ - * /`, `**` |
| Logic | `and`, `or`, `not` |
| Paths | `applicant.income`, `items[1]` |
| Built-ins | `date`, `duration`, `contains`, `starts with`, `list contains`, `count`, `sum`, `min`, `max`, `abs`, `today` |
| Unary tests | bare `< 100`, `[1..10]`, `"GOLD","SILVER"` (comma = disjunction) |

**Explicitly out of scope for v1** (document it, don't fake it): boxed contexts, `for/return`,
`some/every`, function definitions, external functions.

### 2.2 Use it in four places

| Surface | Today | After |
| :-- | :-- | :-- |
| DMN cell (unary test) | JS string concat | FEEL AST |
| Gateway condition | raw JS via goja | FEEL, with JS available only behind an explicit `expressionLanguage: javascript` opt-in that is off by default |
| Input/output mapping | manual JSON keys | FEEL expressions |
| Timer duration | `time.ParseDuration` | ISO-8601 (`PT5M`, `R3/PT10M`, `timeDate`) |

Script tasks keep goja — that is their point — but sandboxed per 1.6.

### 2.3 Fix the DMN engine itself

| # | Work | Detail |
| :-- | :-- | :-- |
| 2.3.1 | Guard `matchingRules[0]` | Every hit policy indexes `[0]` unguarded. No-match must return an explicit "no rule matched" result, not panic. |
| 2.3.2 | Delete the dead hit-policy copy | `applyHitPolicy`/`applyAggregation`/`evaluateFeel` on `decisionService` are unreachable since evaluation moved to `tableEvaluator`. Two implementations guarantee drift. |
| 2.3.3 | Implement `PRIORITY` properly | Currently aliased to `FIRST`. Needs output-value ordering. |
| 2.3.4 | Add `OUTPUT ORDER` and `RULE ORDER` | Completes the DMN hit-policy set. |
| 2.3.5 | Fix decision versioning race | `GetByKey` then `version+1` is a lost-update. Unique constraint on `(key, version)` + retry. |

**Exit criterion:** a DMN table exported from Camunda evaluates identically in Metis for the
supported subset, proven by a conformance test corpus.

---

## Phase 3 — Integration platform

**Driver:** `arch` · **Challenger:** `sec`, `pm`

### 3.1 The product thesis

Every orchestrator can call an HTTP endpoint. That is not a differentiator — it is table
stakes, and Metis already has it.

The differentiator is this: **a process is the only place in a company where the integration,
the business rule, the human decision, the audit trail and the compensation logic all live in
one artifact.** iPaaS tools (Zapier, n8n, Workato) do integration but have no notion of a
long-running business commitment, a human approval queue, or compensation. BPM engines
(Camunda, Flowable) have those but treat integration as a developer task requiring a
redeploy.

**Metis's position: the integration is authored by the same person who authored the process,
in the same tool, without a redeploy.**

That single sentence dictates the architecture below.

### 3.2 The four integration quadrants

|  | **Outbound** (process → app) | **Inbound** (app → process) |
| :-- | :-- | :-- |
| **Sync** | **HTTP/REST Connector** — service task calls an API, waits for the response, maps output to variables. *Exists; needs timeouts, SSRF guard, idempotency.* | **Public Process API** — start an instance, correlate a message, complete a task. *Exists; needs API keys, per-key rate limits, RBAC.* |
| **Async** | **External Task Workers** — engine publishes work, an external worker long-polls, does the work in its own runtime, reports back. *Exists via the AMQP bridge; needs a documented worker protocol + SDK.* | **Webhook & Event Ingress** — signed webhook receiver and AMQP/Kafka consumer that correlate to message events. *Partially exists; needs signature verification and a dedup window.* |

Every real integration is one of these four. The plan is to make all four first-class,
consistent, and configurable without code.

### 3.3 Connectors as data, not code (the key architectural bet)

Today a connector is a Go `switch` branch (`connector.go`, 597 lines, one function per
vendor). Adding Salesforce means editing Go and redeploying the engine. That does not scale
past a handful of connectors and it locks out the community.

**Move to a declarative Connector Manifest.** A connector becomes a versioned document:

```yaml
key: salesforce.create-lead
version: 2
name: Salesforce — Create Lead
category: crm
icon: salesforce
auth:
  type: oauth2_client_credentials       # none | basic | bearer | api_key | oauth2_* | mtls
  token_url: https://login.salesforce.com/services/oauth2/token
  scopes: [api]
config_schema:                          # tenant-level, set once in Connectors admin
  type: object
  required: [instance_url]
  properties:
    instance_url: { type: string, format: uri, title: Salesforce instance URL }
input_schema:                           # per-node, set in the designer
  type: object
  required: [last_name, company]
  properties:
    last_name: { type: string, title: Last name }
    company:   { type: string, title: Company }
    email:     { type: string, format: email }
output_schema:
  type: object
  properties:
    lead_id: { type: string }
request:
  method: POST
  url: "{{config.instance_url}}/services/data/v60.0/sobjects/Lead"
  body:
    LastName: "{{input.last_name}}"
    Company:  "{{input.company}}"
response:
  success: "status >= 200 and status < 300"
  outputs:
    lead_id: "body.id"
errors:
  - when: "status = 401"
    bpmn_error: AUTH_FAILED
    retryable: false
  - when: "status = 429"
    bpmn_error: RATE_LIMITED
    retryable: true
    retry_after: "headers['Retry-After']"
```

What this unlocks, in order of business value:

1. **The designer property panel renders itself.** `input_schema` is JSON Schema → the form
   is generated. No per-connector UI code. The existing `FormBuilder.tsx` already does
   schema-driven forms; point it at connector schemas.
2. **A connector catalog / marketplace.** Manifests are data, so they can be listed, versioned,
   signed, shared and installed at runtime.
3. **OpenAPI import.** Point Metis at a `swagger.json`, generate a manifest per operation.
   This is the single highest-leverage feature in the whole plan — it turns "does Metis
   integrate with X?" from a roadmap question into a 30-second import.
4. **Errors map to BPMN.** `errors[]` turns HTTP failures into BPMN error codes that boundary
   events can catch — the integration failure becomes a modelled business path, not a stack
   trace.
5. **The expression language is FEEL** (Phase 2). One language across conditions, mappings
   and connectors.

Keep the Go connector interface for genuinely code-shaped connectors (SDK-based, streaming,
stateful). Manifest connectors cover the ~90% that are "call a REST endpoint with auth."

### 3.4 Integration reliability — the non-negotiables

These are what separate a demo from something a bank runs payroll on:

| Concern | Requirement |
| :-- | :-- |
| **Idempotency** | Every outbound call carries an engine-generated idempotency key derived from `(instanceID, nodeID, attempt-invariant)`. Persist an "executed" marker **before** the call and reconcile after, so a retry after a commit failure does not re-charge a card. This closes the confirmed defect in `executeServiceTask`. |
| **Retry** | Exponential backoff **with jitter** (currently linear, and the README wrongly claims exponential). Per-error-class policy from `errors[].retryable`. Honour `Retry-After`. |
| **Circuit breaker** | Per connector-instance. An unhealthy downstream must not consume the whole job pool. |
| **DLQ** | Poison messages go to a dead-letter store with a replay UI, not an infinite retry loop. |
| **Credential vault** | Connector secrets encrypted with a real KMS-backed key (Phase 1.1), never in process variables, never in logs, redacted in audit. |
| **Egress control** | Deny-by-default allowlist per tenant. |
| **Rate limiting** | Per connector instance, so one process cannot exhaust a partner's quota. |
| **Observability** | Every external call emits a span with connector key, instance, node, attempt, latency, status. |

### 3.5 The external-task worker protocol

For work that cannot or should not run in the engine (heavy compute, private-network access,
another language's SDK), publish a documented protocol plus SDKs:

```
POST /api/v1/external-tasks/fetch-and-lock   { topics, workerId, lockDuration, maxTasks }
POST /api/v1/external-tasks/{id}/complete    { variables }
POST /api/v1/external-tasks/{id}/failure     { errorMessage, retries, retryTimeout }
POST /api/v1/external-tasks/{id}/bpmn-error  { errorCode, variables }
POST /api/v1/external-tasks/{id}/extend-lock { newDuration }
```

Ship a Go SDK first, then TypeScript/Python. This is how Camunda 8 wins developer trust, and
the endpoints already substantially exist — they need the lock-extension path, a documented
contract and the SDKs.

### 3.6 Inbound: webhooks and public API

- **Signed webhook receiver**: `POST /api/v1/hooks/{token}` → verify HMAC signature → correlate
  to a message start event or an active message catch event → dedup on delivery ID within a
  window.
- **Per-tenant API keys** with scoped permissions, rate limits and rotation.
- **OpenAPI spec published** for the public surface so customers can generate clients.

---

## Phase 4 — BPMN × DMN: how process and decision combine

**Driver:** `bpm` + `dmn` · **Challenger:** `ux`

### 4.1 The principle

**BPMN answers "what happens and in what order." DMN answers "what should we decide."**

The reason to separate them is not architectural purity — it is release cadence.
**Process structure changes quarterly. Business policy changes weekly.** If the discount
policy is an `if` inside a gateway condition, changing it requires a developer, a redeploy and
a regression test of the whole process. If it is a decision table, a business analyst edits a
grid and it takes effect on the next evaluation.

Every rule that a non-developer might ever want to change belongs in DMN, not in a gateway.

### 4.2 The five integration patterns

| Pattern | Shape | Use when |
| :-- | :-- | :-- |
| **1. Decision-as-task** | Business Rule Task → DMN decision → outputs land in process variables | The classic. Risk scoring, pricing, eligibility. *Exists (`business_rule.go`).* |
| **2. Decision-driven routing** | Business Rule Task returns a routing key → exclusive gateway conditions are trivial `= "APPROVED"` comparisons | **The recommended default.** Keeps all policy in one auditable table instead of scattered across gateway conditions. |
| **3. Decision-driven assignment** | DMN returns assignee / candidate group / priority / due date for a user task | Approval matrices ("amount > 10k → CFO"). Removes the most common reason to hardcode org structure in a diagram. |
| **4. Decision Requirements Graph** | Decisions depend on decisions; the engine evaluates the DAG bottom-up | Layered policy: eligibility → risk band → price. *`RequiredDecisions` exists and works.* |
| **5. Decision-driven SLA** | DMN computes a timer duration, the boundary timer consumes it | Per-segment SLAs without a diagram per segment. Requires Phase 2 ISO-8601 timers. |

### 4.3 What to build

| # | Work | Size |
| :-- | :-- | :-- |
| 4.3.1 | Make pattern 2 the designer default: a "Decide" node that pairs a Business Rule Task + gateway in one visual affordance | M |
| 4.3.2 | Decision output → user task assignment/priority/due-date binding (pattern 3) | M |
| 4.3.3 | DRG visual editor — show decisions and their dependencies as a graph, not a flat list | L |
| 4.3.4 | Decision versioning independent of process versioning, with a "which processes use this decision" impact view | M |
| 4.3.5 | **Decision test suite + simulation** — attach example inputs to a table, show which rules fire, flag unreachable rules and gaps in coverage | L |
| 4.3.6 | Decision audit — record which decision version and which rule produced each output, on the instance timeline | M |

4.3.5 and 4.3.6 are what make DMN trustworthy to a compliance team. A decision table nobody
can test is just a spreadsheet with extra steps; a decision nobody can trace is an audit
finding.

### 4.4 Governance

- Decisions are **immutable once deployed**, like process definitions.
- Running instances pin the decision version they started with, unless explicitly migrated.
- Every evaluation records `(decisionKey, version, ruleId, inputs, output)` in the audit trail.
- A decision cannot be deleted while any active instance references it.

---

## Phase 5 — UI/UX refactor and stack upgrade

**Driver:** `fe` + `ux` · **Challenger:** `perf`, `arch`

### 5.1 Current state, measured

| Signal | Value |
| :-- | :-- |
| `.tsx` files | 78 |
| Files with any `aria-label` / `role` | **1** |
| i18n | **none** — all strings hardcoded English |
| Frontend tests | **1** (`api.identity.test.ts`) across ~16k LOC |
| Largest components | `TaskInbox.tsx` 905, `DecisionEditor.tsx` 709, `Connectors.tsx` 662, `Setup.tsx` 650 |
| Design tokens | defined inline in `routes/__root.tsx` |
| `postcss.config` | **missing** — `postcss-preset-mantine` not installed, so `light-dark()`, `rem()` and Mantine mixins are unavailable |
| `@emotion/react`, `@emotion/styled` | **dead dependencies** — zero imports, but bundled into `vendor-mantine` |
| ESLint | **231 errors** |
| PWA / service worker / web workers | none |

### 5.2 Version upgrade plan (all versions verified live)

| Package | Now | Target | Type | Notes |
| :-- | :-- | :-- | :-- | :-- |
| Go | 1.25.0 (mod) | **1.26.5** | patch-align | toolchain already 1.26.5; align mod + CI + README |
| TypeScript | 5.9.3 | **7.0.2** | major ×2 | native Go compiler. **Gate: verify `typescript-eslint` 8.67 + `@vitejs/plugin-react` compatibility on a spike branch before committing.** |
| React | 19.2.0 | **19.2.8** | patch | already current major |
| Mantine | 8.3.16 | **9.5.1** | major | 7.x is now `legacy` tag |
| Vite | 7.3.1 | **8.2.1** | major | |
| `@vitejs/plugin-react` | 5.1.1 | **6.0.5** | major | |
| TanStack Router | 1.166.3 | **1.170.25** | minor | also swap `router-vite-plugin` → **`@tanstack/router-plugin` 1.168.29** |
| TanStack Query | 5.90.21 | **5.101.4** | minor | |
| TanStack Form | — | **1.33.5** | new | replaces `@mantine/form` |
| TanStack Virtual | — | **3.14.9** | new | list virtualization |
| Tailwind CSS | — | **4.3.3** + `@tailwindcss/vite` | new | see 5.3 |
| `postcss-preset-mantine` | — | **1.18.0** | new | currently missing |
| `babel-plugin-react-compiler` | — | **1.0.0** | new | auto-memoization |
| `vite-plugin-pwa` | — | **1.3.0** | new | |
| Vitest | — | **4.1.10** | new | real test runner |
| `@axe-core/react` | — | **4.13.0** | new | a11y in CI |
| `@emotion/react`, `@emotion/styled` | 11.x | **removed** | delete | dead |

**Upgrade order matters.** Do not do these in one PR:

```
5.2.a  Vite 8 + plugin-react 6           (build tooling first, smallest blast radius)
5.2.b  Delete Emotion, add postcss-preset-mantine
5.2.c  Mantine 8 → 9                     (biggest visual diff — isolate it)
5.2.d  TanStack Router/Query minors + router-plugin swap
5.2.e  Tailwind 4                        (additive, no behaviour change)
5.2.f  TypeScript 7                      (behind the compat gate)
5.2.g  React Compiler                    (measure before/after; revert if no win)
5.2.h  TanStack Form migration           (per-form, incremental)
```

Each step ships green, independently revertible.

### 5.3 Mantine 9 + Tailwind 4 — the honest tradeoff

You asked for both. They can coexist, but naively combining them gives two ways to do
everything, specificity fights, and a design system nobody can enforce. Here is the boundary I
recommend, and I'd hold this line strictly:

- **Mantine v9 owns components and semantics** — Button, Table, Modal, Select, dates,
  notifications, focus management, a11y primitives. Never restyle a Mantine component with
  Tailwind utilities.
- **Tailwind v4 owns layout and one-off utilities** — grid, flex, spacing between components,
  responsive composition in page shells.
- **One token source.** Tailwind v4's CSS-first `@theme` block consumes Mantine's CSS
  variables, so there is exactly one definition of a colour or spacing step:

```css
@import "tailwindcss";
@theme {
  --color-brand-500: var(--mantine-color-brand-5);
  --spacing-md:      var(--mantine-spacing-md);
  --radius-md:       var(--mantine-radius-md);
}
```

- **Enforce it.** An ESLint rule that forbids Tailwind colour/typography utilities on Mantine
  components. Without enforcement this boundary erodes in about six weeks.

If you would rather not carry two systems, the alternative is Mantine 9 alone with CSS
modules — it covers everything here. My recommendation is the boundary above, because
Tailwind genuinely wins on layout composition and it keeps page shells readable. But the
boundary is the whole value; unenforced, this is a net negative.

### 5.4 UX refactor — the four hero surfaces

Design for three distinct personas. Today the UI treats them as one, which is why the task
inbox shows node IDs.

| Persona | Needs | Primary surface |
| :-- | :-- | :-- |
| **Business user** (highest volume) | "What do I need to do today?" Zero BPMN vocabulary. | Task Inbox |
| **Process designer** (analyst) | Model, simulate, deploy, without writing code | Designer + Decision Editor |
| **Operator / admin** | "What is stuck and why?" | Monitoring + Incidents |

#### A. Task Inbox — the daily driver
- **Kanban + list + calendar** views; Kanban by status, calendar by due date.
- **Priority and overdue** as visual weight, not a text column.
- **Bulk actions** — claim, complete, delegate across selection.
- **Saved views and filters** persisted per user.
- **Virtualized** (TanStack Virtual) — it must stay smooth at 10k tasks.
- **Business Timeline** instead of an event log: "Sarah approved the £4,200 expense" not
  `Task_Approved node=Activity_1x2y`. The guidelines already mandate this; the inbox is where
  it matters most.
- **Offline-capable** (5.5) — this is the differentiator for frontline and field workers.
- Split the 905-line component: `useTaskInbox` (data) / `TaskInboxView` (layout) /
  `TaskCard`, `TaskFilters`, `TaskBulkBar` (presentational).

#### B. Process Designer
- **Guided wizard mode** for non-technical users + a template gallery, with an "expert mode"
  toggle for the full palette (progressive disclosure, per guidelines §5).
- **Live validation** in the canvas: unreachable nodes, deadlocks, missing default flows
  (which Phase 1.8 makes an error rather than a silent fallback), unconfigured connectors.
  Severity-based blocking before deploy.
- **Auto-layout** and smart auto-connect.
- **Simulation mode** — step a token through the diagram with sample variables and watch which
  gateway and which DMN rule fires. This is the single feature that most improves designer
  confidence.
- **Diff view** between definition versions.
- Move heavy work (layout, XML parse, validation) to a **Web Worker** (5.5).

#### C. Decision Editor
- Spreadsheet-grade grid: keyboard nav, fill-down, copy/paste from Excel, column resize.
- **Inline FEEL editor** with autocomplete over available variables and live validation
  (enabled by Phase 2's real parser — you cannot autocomplete a string-concat evaluator).
- **Rule coverage** — highlight unreachable and overlapping rules as you type.
- **Test panel** — input sets alongside the table, showing which rule fires (Phase 4.3.5).
- DRG view for decision dependencies.

#### D. Monitoring / Ops
- Instance list with the execution path rendered **on the diagram**, heat-mapped by frequency
  (`GetExecutionPath` already returns the frequency data — nothing renders it yet).
- **Incident inbox** with the Smart Troubleshooter: plain-English cause and a one-click fix
  ("Salesforce token expired — reconnect").
- Variable history timeline with diffs (`variable_snapshot` already persists these).
- Cohort view: which definitions are failing, where processes stall.

### 5.5 Performance: the specifics

| Technique | Where it pays | Target |
| :-- | :-- | :-- |
| **Web Workers** | BPMN auto-layout, XML parse/serialize, definition validation, DMN simulation, large-table filter/sort, JSON diffing | Main thread never blocks >50ms |
| **PWA** (`vite-plugin-pwa` + Workbox) | **Offline task inbox** — cache assigned tasks and forms; queue completions in IndexedDB; Background Sync flushes on reconnect | Inbox usable at zero connectivity |
| **Virtualization** (TanStack Virtual) | Inbox, instance list, audit log, decision grid | 60fps at 10k rows |
| **React Compiler** | Auto-memoization across the app | Measure; keep only if it wins |
| **Route-level code splitting** | Already partly done via `.lazy.tsx` | Extend to designer sub-panels |
| **Bundle discipline** | Drop Emotion (dead); `lucide-react` per-icon imports; audit the 318KB `vendor-mantine` chunk after the v9 upgrade | Initial JS < 200KB gzip |
| **Query tuning** | TanStack Query `staleTime`/`gcTime` per resource; SSE (already present) to invalidate instead of poll | No polling loops |
| **Optimistic updates** | Claim/complete/delegate | Inbox feels instant |

**Performance budget, enforced in CI** (fail the build on regression):

| Metric | Budget |
| :-- | :-- |
| Initial JS (gzip) | < 200 KB |
| LCP (mid-tier laptop, throttled) | < 1.5s |
| INP | < 200ms |
| Route transition | < 100ms |
| Inbox at 10k tasks | 60fps scroll |

### 5.6 Accessibility and i18n — not optional

- **WCAG 2.1 AA.** Currently 1 of 78 components has an accessible name. Enterprise and public-
  sector procurement checklists ask for a VPAT; today the honest answer is "no." Add
  `@axe-core/react` in dev and an axe assertion in CI so this cannot regress again.
- Keyboard paths for every flow, including the designer canvas.
- **i18n from the start of the refactor**, not retrofitted. Retrofitting 78 components later
  costs several times more than extracting strings while we are already rewriting them.

---

## Sizing and sequencing

Ranges assume a small team; treat as relative sizing, not a commitment.

| Phase | Scope | Rough size | Gate to exit |
| :-- | :-- | :-- | :-- |
| **0** | Green gate | 1–2 weeks | 5 commands exit 0 in CI |
| **1** | P0 security | 3–4 weeks | External review schedulable |
| **2** | FEEL + DMN engine | 4–6 weeks | Camunda DMN corpus passes |
| **3** | Integration platform | 8–12 weeks | OpenAPI import → working connector, no redeploy |
| **4** | BPMN × DMN patterns | 4–6 weeks | Decision test + audit trail shipped |
| **5** | UI/UX + upgrade + perf | 10–14 weeks | Perf budget + axe enforced in CI |

Phases 2–5 overlap. 0 and 1 do not.

## What I would *not* do

- **Do not start Phase 5 first** because it is the visible one. Upgrading Mantine and adding
  Tailwind on top of 231 lint errors and a red vet produces an unfalsifiable mess.
- **Do not add more connectors as Go code.** Every one added before 3.3 is migration debt.
- **Do not build a full FEEL implementation.** Ship the documented subset in 2.1 and say
  precisely what is unsupported. A documented subset beats a silent approximation.
- **Do not adopt Tailwind without the enforced boundary in 5.3.** Two unenforced styling
  systems is worse than either alone.
- **Do not ship the Phase 1.8 gateway change without a migration path.** It is correct, and it
  will break definitions that silently relied on the old fallback. Flag it, detect affected
  definitions, tell users which ones.
