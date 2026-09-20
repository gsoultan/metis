# Metis BPM — Security Remediation Plan

Companion to [`execution-plan.md`](execution-plan.md) (phase order) and
[`../SECURITY.md`](../SECURITY.md) (posture and what has already been audited).
This document covers the **four gaps that remain after Phase 1**, in the order
they should be closed and why that order and not another.

Written 2026-09-19 against `v0.3.0-1-g130d896`. Every claim below was verified
against the source or by running the command shown, not taken from a prior
document.

---

## What was verified green first

So the gaps below are read against a known baseline, not a guess:

```
make vuln  (govulncheck ./...)
  === Symbol Results ===
  No vulnerabilities found.
  Your code is affected by 0 vulnerabilities.
  This scan also found 0 vulnerabilities in packages you import and 1
  vulnerability in modules you require, but your code doesn't appear to call these.
  [exited with code 0]

make gate   → ✅ gate green (0 FAIL, 0 DATA RACE, 517 UI tests pass)
```

Six controls were read in the source and are correct. They are listed here so a
reviewer does not re-tread them, and so a regression in any of them is
recognisable as a regression:

| Control | Where | Why it is right |
| :-- | :-- | :-- |
| SSRF egress guard | `internal/pkg/httpclient/client.go` | `ControlContext` runs **after** DNS resolution, immediately before connect, once per address attempted — so it closes DNS rebinding. Covers loopback, link-local (`169.254.169.254`), RFC1918, and RFC 6598 CGNAT, which `net.IP.IsPrivate` does not. |
| JWT algorithm confusion | `server/domains/services/impl/user.go:141` | `jwt.Parse` asserts `token.Method.(*jwt.SigningMethodHMAC)`. HS256 only. |
| Password storage | `server/domains/services/impl/user.go:74,113` | bcrypt at default cost, plus a dummy-hash compare on the unknown-user path so timing does not enumerate usernames. |
| Tenant derivation | `server/interceptors/tenant/resolver.go` | Derived from the authenticated principal loaded from the database, not from `X-Organization-ID`. `ErrUnresolvedTenant` refuses the OIDC case, where claims carry roles but no membership list. |
| Absent-constraint-denies | `server/domains/services/impl/task.go:148,268` | Claim and complete both route through `authorizeCandidate`. The old `task.Assignee != nil &&` guard, which let any authenticated user complete any unclaimed task, is closed. |
| Endpoint authorization wiring | `tests/endpointwiring/wiring_test.go` | A **static** check that parses the source and asserts every declared `endpoint.Endpoint` field is assigned from an authorization chain. Correct shape: the wrapping is a compile-time fact, so a runtime assertion would need every endpoint exercised to find the one that is missing. |

---

## P0.1 — Roll out the strict tenant scope

**The gap.** `METIS_FEATURE_STRICT_TENANT_SCOPE` ships **off**. With it off, a
code path that fails to resolve a tenant gets unscoped access — every
organization's rows. On a multi-tenant installation that is the difference
between a bug and a disclosure.

**Why it is first.** Not because it is the most work — it is not. Because step 5
is *"leave it on through a full release cycle before treating it as settled."*
The long pole is calendar time, so every week it is not started is a week added
to the end. The engineering is already built: the procedure is in
[`../docs/strict-tenant-scope.md`](../docs/strict-tenant-scope.md), the warning
log names the path that has to change, and `gorms.DeniedSites()` returns the
same list as a value for tests to assert on.

**The work:**

1. Set the flag in **staging only**. Confirm the startup log states it — flags
   are read at startup precisely so that "turned it on and saw nothing" is
   distinguishable from mistyping the variable name.
2. Exercise breadth, not depth: deploy a definition, start instances, let timers
   fire, work the inbox, run a decision, drive a connector, correlate a message.
   You are looking for a path nobody thought about, so use the features you
   actually use.
3. For each `WRN A repository query carried neither a tenant nor a system
   identity`, fix the `called_from` path. Never the `repository`.
4. **The judgment call that turns this fix into a vulnerability if it is made
   wrong:** background work that legitimately spans tenants takes
   `entities.WithSystemContext`. Anything serving a request needs a *resolved
   tenant*. Marking a request path as system work silences the warning and makes
   the cross-tenant access permanent.
5. Every fix ships a test in `tests/strictscope` using `assertNothingWasDenied`,
   which turns the same diagnostic into a build failure.

**Explicitly not in scope.** Do not try to make `go test ./...` pass with the
flag on. The ~130 failures are tests calling services with a bare `t.Context()`,
which no production path does. A red run nobody can act on is a run people learn
to ignore — which is why `make strict-scope` names packages rather than `./...`.

**Exit gate.** Zero denied sites through a full release cycle in staging → flip
the default → delete the flag.

**Size.** Small effort, ~1 release cycle calendar. Starts now, runs in background.

---

## P0.2 — Bound the script-task blast radius

**The gap.** The script sandbox has an interrupt, a wall-clock budget, an
abandon-after-grace path, `SetMaxCallStackSize`, `eval`/`Function` deleted, and
panic recovery. It has **no memory bound**, and `server/domains/logic/sandbox.go`
says so in as many words:

> This bounds worker starvation, not memory. An abandoned script goes on
> allocating until it finishes, and goja offers no heap limit to cap it with.

Verified against the pinned runtime — `github.com/dop251/goja
v0.0.0-20260305124333-6a7976c22267` exposes no `SetMemoryLimit` and no
`MemUsage`. This is not a missed one-line call; the API does not exist.

The measurement in the sandbox comments: the gateway condition
`new Array(1e9).join('x')` ran for **37 seconds against a 200ms budget** — 188×
over, because goja honours interrupts only between statements and never inside a
single long native call.

**This is the one veto in `AGENTS.md` §2 `sec` that the codebase does not meet.**

**The constraint that shapes the fix.** Gateway conditions could be defaulted
*off* because FEEL replaced them. **Script tasks have no replacement**, so
copying that pattern breaks every definition that uses one. The mitigation has
to be staged:

- **(a) Visibility — ~1 day.** Add `GET /api/v1/definitions/script-tasks`,
  mirroring the existing `definitions/javascript-conditions` endpoint. A risk
  whose blast radius cannot be enumerated cannot be managed.
- **(b) Containment — ~2–3 days.** Cap concurrent script executions below
  `METIS_JOB_WORKERS`, and set a container memory limit so the kernel kills one
  pod rather than degrading the node. With the existing 5-minute job lease and
  `METIS_SHUTDOWN_DRAIN`, a killed pod's claimed jobs are retried. This converts
  *unbounded host memory exhaustion* into *one pod restart*. That is a genuine
  reduction, and it is not a fix.
- **(c) Resolution — larger.** Either run scripts out-of-process under an
  rlimit/cgroup, or extend FEEL to cover what script tasks are actually used
  for, then gate JavaScript the way conditions were gated.

Do (a) and (b) now. **Let (a)'s data decide whether (c) is FEEL or process
isolation** — do not guess which, and do not build both.

**Threat model note.** Authoring a script task requires the designer role, so
this is a privileged-user denial of service, not an anonymous one. That lowers
its severity; it does not remove it, because "designer" is a role granted to
business analysts, not to operators.

**Exit gate.** (a) an endpoint listing every definition with a script task.
(b) a test that fills the script slots and asserts the job worker still claims
non-script jobs.

---

## P1.1 — SAST in CI

**The gap.** `.golangci.yml` enables a correctness-focused set — `errcheck`,
`bodyclose`, `noctx`, `contextcheck`, `rowserrcheck`, `sqlclosecheck`, `nilerr`,
`errorlint`, `gocritic`, `revive`. There is **no `gosec`, no CodeQL, no
semgrep**. CI has `govulncheck` (Go dependencies), `bun audit` (UI
dependencies), `gitleaks` (secrets) and `trivy` (image), so `roadmap.md` §5.6's
*"SAST + dependency + secret scanning in CI"* is two-thirds done.

**Ordering matters.** Add the linter **before** the golangci-lint burn-down.
Burning down a backlog and then enabling a linter that finds more is wasted
work.

Use `only-new-issues`, the same mechanism that already baselines golangci-lint,
so the existing backlog does not block the pipeline.

**Size.** ~1 day. **Exit gate.** A new `gosec` finding fails CI; the pre-existing
set is baselined and counted.

---

## P1.2 — Login throttle

**The gap.** There is no per-account throttle and no lockout. Only the global
per-IP HTTP limiter applies. bcrypt's cost and the constant-time compare make
single-account brute force slow, but distributed credential stuffing across many
source addresses is not specifically defended.

**The easy part is already done.** `server/interceptors/security/rate_limit_interceptor.go`
already handles the hard problems correctly: `clientKey` honours
`X-Forwarded-For` only from trusted proxies (the "rate limiter disabled by a
header" bug was found and fixed), and `ShareVia` pools the count across replicas
so N replicas do not admit N× the limit.

**The work.** A tighter per-account bucket on the authentication endpoint
reusing that machinery, with exponential backoff per account rather than a hard
lockout — a hard lockout is itself a denial of service against a known username.

**Size.** ~2 days. **Exit gate.** A test asserting the Nth consecutive failure
for one account is refused regardless of source address.

---

## P2 — Assurance

**External review.** Phase 1's own exit gate in `execution-plan.md` was
*"external review schedulable"* — schedulable, not completed. Schedule it
**after P0.1 lands**, so the auditor tests the strict scope rather than the
fail-open default. Hand them `SECURITY.md` §"What has already been looked at" as
explicit out-of-scope, so the engagement buys new coverage rather than
re-treading ten known issues.

**golangci-lint → 0. Done.** The 799 figure was stale — every linter but
`gosec` already ran clean, which `.golangci.yml`'s own adoption note says. What
remained was the 61 `gosec` findings that enabling it introduced, and those are
closed: three real defects fixed, the rest annotated with a stated reason.

---

## Sequencing

```
P0.1 strict tenant scope   ├──────── staging soak ────────┤ ← calendar-bound, start first
P0.2a script-task listing  ├──┤
P0.2b containment             ├────┤
P1.1 SAST in CI               ├──┤        ← before any lint burn-down
P1.2 login throttle              ├───┤
P2   external review                      ├──── after P0.1 ────┤
```

P0.1 is calendar-bound and everything else is effort-bound, so P0.1 starts first
and the rest proceed alongside it.

## What changes this plan

| Deployment target | Consequence |
| :-- | :-- |
| **Single-tenant, single-replica** | P0.1 drops from blocking to hygiene. Ship on P0.2(a)+(b). |
| **Multi-tenant SaaS** | P0.1 is blocking. Add shared circuit-breaker and rate-limit state across replicas — [`../docs/recovery.md`](../docs/recovery.md) §2.1 lists what is still per-process. |

---

## Status — 2026-09-20

| Item | State |
| :-- | :-- |
| **P0.1** strict tenant scope | Steps 1–4 exercised, no denials found. **Not cleared**: 13 packages still fail with the flag on, and the gate is a staging soak. The flag ships off. |
| **P0.2(a)** script-task inventory | **Done.** `GET /api/v1/definitions/script-tasks`, behind the designer chain — stricter than the javascript-conditions worklist beside it, because this returns every script *body* in the tenant in one call. |
| **P0.2(b)** containment | **Done.** `METIS_SCRIPT_CONCURRENCY` (default 4, below `METIS_JOB_WORKERS`) bounds how many scripts allocate at once; a slot is held until the script actually finishes, so a runaway that ignored its interrupt is still counted. The container memory limit already existed. |
| **P0.2(c)** resolution | **Not started, deliberately.** It is the choice between a FEEL replacement and process isolation, and (a) exists so that choice is made from the scripts an installation actually has. Run the endpoint first. |
| **P1.1** SAST in CI | **Done.** `gosec` enabled in `.golangci.yml`, which CI already runs with `--new-from-merge-base`. 21 pre-existing findings, triaged rather than baselined blind; the one real defect — an unbounded multipart body that spilled past its in-memory budget to disk — is fixed. |
| **P1.2** login throttle | **Done.** `internal/pkg/loginthrottle`: per-account exponential backoff, LRU-bounded because the key is attacker-supplied. Backoff rather than lockout, because a lockout is a denial of service against any guessable username. |
| **P2** external review | **Cannot be done from here.** Needs a third party. Schedule after P0.1. |
| **P2** golangci-lint burn-down | **Done.** `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` reports **0 issues**. The 799 was stale; the real remainder was the 61 `gosec` findings that adopting SAST introduced. |

## The standing lesson

`SECURITY.md` records that all ten issues found in the 2026-08-30 → 2026-09-04
audit window **had passing tests over them**. None was found by a test going
red; each was found by asking what a specific piece of code would do with
hostile input, and then running it.

A green gate is evidence of no *known* failure. It is not evidence of security,
and no item in this plan should be closed on the strength of one.
