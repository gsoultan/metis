### Gobpm Production Roadmap (Scalability, Reliability, Security, UX)

> **Sequenced execution plan:** see [`execution-plan.md`](execution-plan.md) — Phases 0–5 with
> ordering, sizing, verified dependency versions, and exit gates. This file holds the *themes*;
> `execution-plan.md` holds the *order*. Phase 0 (green verification gate) blocks all other work.

#### 1. Production SLO Targets (Non-Negotiable)

1. API latency targets:
   - `p95 < 150ms` for common reads.
   - `p95 < 500ms` for workflow actions.
2. Throughput target:
   - Sustained `10k+` events/minute.
3. Reliability target:
   - `< 0.1%` `5xx` error budget.
   - `99.9%+` availability.
4. Recovery target:
   - Explicit `RTO`/`RPO` per environment.

#### 2. Core Backend Architecture

1. Keep `ServiceFacade` thin and orchestration-only.
2. Enforce small, consumer-centric interfaces in contracts.
3. Use patterns intentionally:
   - `Repository` + `Unit of Work` for multi-step writes.
   - `Strategy` for swappable algorithms/backends.
   - `Adapter` for external systems.
   - `Decorator` for logging, retry, tracing, metrics.
   - `Observer` for domain events.

#### 3. Performance & Memory Strategy

1. Profile first (`pprof` CPU + heap under load) before tuning.
2. Remove avoidable `O(n^2)` loops with map-based lookups.
3. Preallocate slices where capacity is known.
4. Minimize re-marshal cycles and transient allocations on hot paths.
5. Add backpressure with bounded queues, worker limits, and rate limiting.

#### 4. Heavy-Traffic / Heavy-Workflow Readiness

1. Add idempotency keys for externally triggered commands.
2. Use retry with exponential backoff + jitter.
3. Add DLQ handling for poison messages.
4. Partition execution by tenant/instance key where applicable.
5. Propagate timeout/cancellation with context through all layers.

#### 5. Security Hardening

1. Enforce authn/authz (`RBAC`, optional `ABAC`).
2. Ensure tenant isolation in repositories and queries.
3. Keep all DB access parameterized.
4. Redact secrets/PII from logs.
5. Add API abuse controls:
   - Request size limiting.
   - Rate limiting.
6. Include SAST + dependency + secret scanning in CI.

#### 6. Reliability & Bug Reduction

1. Maintain test pyramid:
   - Unit tests (table-driven).
   - Integration tests.
   - Contract tests for connectors.
   - E2E BPM scenarios.
2. Enable `go test -race` in CI.
3. Add fuzz tests for parsers/forms/expressions.
4. Add outage simulations (DB/broker/network).
5. Use feature flags + canary rollout for risky changes.

#### 7. User-Friendly UX Roadmap

##### 🔴 High Priority — Core UX Gaps

1. Guided Process Designer Wizard:
   - Step-by-step mode for non-technical users.
   - Template gallery.
   - Inline glossary and smart auto-connect.
2. Enhanced Smart Troubleshooter:
   - Whole-process validation (deadlocks, unreachable nodes).
   - Pre-deployment checklist with pass/fail.
   - Severity-based blocking.
3. Task Inbox UX Overhaul:
   - Kanban board.
   - Priority badges and overdue countdown.
   - Bulk task actions.
   - Inline business timeline.
4. Form Builder Enhancement:
   - Drag-and-drop designer + preview.
   - Visual condition builder.
   - Plain-English validation messages.

##### 🟡 Medium Priority — Operational Excellence

5. Process Monitoring Dashboard:
   - Live process heatmap.
   - SLA/compliance reporting.
   - Export (PDF/CSV).
6. Notification System:
   - In-app center with unread count.
   - Assignment/incident alerts.
   - Email/webhook notifications.
7. RBAC UI:
   - Visual role editor.
   - Group/org-scoped access.
8. Process Versioning & Migration UI:
   - Version history with visual diff.
   - Rollback + migration wizard.

##### 🟢 Lower Priority — Delight Features

9. Connector Marketplace (plug & play).
10. Decision Table Visual Editor.
11. Progressive Disclosure (`Expert Mode`).
12. Onboarding & Help System.

#### 8. 90-Day Execution Plan

1. Phase 1 (0-30d):
   - Baseline profiling + SLO dashboard.
   - Address top 5 bottlenecks.
   - Fix critical security gaps.
2. Phase 2 (31-60d):
   - Architecture cleanup (`contracts`, transactions, idempotency).
   - Ship high-value UX usability improvements.
3. Phase 3 (61-90d):
   - Load/chaos testing.
   - Canary rollout + hardening.
   - Playbooks and documentation.

#### 9. Roadmap Completion Checklist (Option 1 Tracker)

- [x] 1. Production SLO Targets (Non-Negotiable)
  - [x] API latency targets defined **and measured** — `tests/slo` drives the real
        HTTP handler and fails if a target is missed. Measured 2026-08-26 against
        live PostgreSQL 17: reads p95 **11.1ms** against the 150ms target,
        workflow actions p95 **13.8ms** against 500ms, 5xx rate **0.000%**
        against the 0.1% budget. Asserting production numbers in-process is not
        flaky because it leaves one to two orders of magnitude of headroom; what
        it catches is the regression that eats an order of magnitude.
  - [x] Throughput target defined **and measured** — 170,569 process starts/min
        on PostgreSQL, 369,049/min on SQLite, against the 10k/min target. Reported
        rather than asserted: throughput is a property of the hardware, and a
        threshold would either prove nothing or fail on a busy machine.
  - [x] Reliability target defined and measured (0.000% 5xx over the runs above).
  - [x] Recovery target finalized with explicit per-environment `RTO`/`RPO` values —
        see [`docs/recovery.md`](../docs/recovery.md). Production RPO 5min / RTO 1h,
        with the backup, restore and quarterly rehearsal procedures behind them.
- [ ] 2. Core Backend Architecture
  - [ ] `ServiceFacade` orchestration-only compliance verified across domains.
  - [ ] Small, consumer-centric interface compliance audit completed.
  - [ ] Pattern usage audit (`Repository`, `UnitOfWork`, `Strategy`, `Adapter`, `Decorator`, `Observer`) completed.
- [ ] 3. Performance & Memory Strategy
  - [x] `pprof` CPU/heap baseline under load established.
  - [x] `P1-OPT-01` setup-status request-path overhead trim completed (middleware wrap reuse).
  - [x] `P1-OPT-02` connection-churn guardrails completed (explicit HTTP server keep-alive settings + sustained-load verification).
  - [x] `P1-OPT-03` regex compile hotspot audit and caching/precompile optimization completed.
  - [x] `P1-OPT-04` setup-status rate-limit transient-overhead reduction completed (in-place window updates + client key fast-path parsing).
  - [x] `P1-OPT-05` setup-status auth public-path lookup optimization completed (linear scan replaced with map lookup + auth header parse allocation trim).
  - [x] `O(n^2)` hot loops replaced with map-based lookups where needed.
  - [x] Slice preallocation pass completed on profiled hot paths.
  - [x] Re-marshal/transient allocation reduction pass completed.
  - [x] `P1-OPT-08` request-path logging/serialization optimization completed (`Dur` field serialization + single failer evaluation).
  - [x] Backpressure package complete (bounded queues + worker limits + rate limiting).
- [x] 4. Heavy-Traffic / Heavy-Workflow Readiness
  - [x] Idempotency keys for externally triggered commands.
  - [x] Retry policy with exponential backoff + jitter.
  - [x] DLQ handling for poison messages.
  - [x] Partition execution by tenant/instance key.
  - [x] Context timeout/cancellation propagation audit.
- [x] 5. Security Hardening
  - [x] Authn/Authz parity (`RBAC` + optional `ABAC`) audit completed.
  - [x] Tenant isolation verification completed in repositories/queries — every
        project-owned table is scoped on reads, writes and creates, proven on
        SQLite, PostgreSQL and MySQL by `tests/tenant/isolation_test.go`. The
        repository layer still fails open with no `TenantContext`, which is now
        defence in depth rather than a live hole: unresolvable principals are
        refused at the resolver, and the public chain's membership is asserted
        by test. See `execution-plan.md` §Status.
  - [x] DB parameterization audit completed.
  - [x] Secret/PII redaction implemented for outward errors/log paths.
  - [x] API abuse controls implemented (request-size limit + rate limit interceptors).
  - [x] Security/reliability CI scanning baseline implemented (`go vet`, `go test -race`, `govulncheck`, `gitleaks`).
- [ ] 6. Reliability & Bug Reduction
  - [x] Full test-pyramid baseline complete (unit + integration + contract + E2E).
        The contract tier is `tests/connector/contract_test.go`: what each
        connector puts on the wire and how it reads what comes back — a GET
        carrying no body, a non-JSON 200 being a result rather than a failure, a
        manifest's error rules deciding before its success condition (plenty of
        APIs report failure with a 200), and the BPMN error code and
        retryability a boundary event acts on. It also pins the egress policy,
        which is the promise most easily lost: a connector URL can come from an
        installed manifest, and without the policy that is a request forger
        pointed at the private network.
  - [x] `go test -race` enabled in CI workflow.
  - [x] Fuzz tests added for parsers/forms/expressions — `tests/fuzz`, five targets
        over the BPMN XML parser, its round trip, the condition chain and FEEL.
        Found the script-sandbox DoS and the `Parse` nil-contract defect.
  - [x] Outage simulation suite added — `tests/outage`: a severable TCP proxy cuts the
        database under a running engine; asserts fail-fast during the outage, a truthful
        503 from `/readyz`, and full recovery afterwards (parked external task completes,
        the instance finishes, fresh work starts). Broker reconnect is covered at the unit
        level in `messaging_test.go`; the network dimension is the proxy itself.
  - [~] Feature-flag mechanism defined and integrated — `internal/pkg/features`,
        used by the strict tenant scope and the system-identity work. **Canary
        rollout is not built**: there is no traffic-splitting or staged-cohort
        mechanism, so a flag is on or off for the whole installation. The shipped
        defaults are pinned by test (`TestSecurityDefaults`), because changing
        either one is a security decision with a rollout plan behind it rather
        than a tweak.
- [ ] 7. User-Friendly UX Roadmap
  - [x] Business Timeline audit log: `AuditWriter` contract + `narrativeFor` narrative generator + lifecycle hooks for all task events (Claim/Unclaim/Complete/Assign/Delegate/Create).
  - [x] Task Inbox UX overhaul: priority badges, overdue countdown, bulk actions.
        Urgency is computed once in `ui/src/domain/taskUrgency.ts` from the two
        things that make a task urgent — late, or important — and rendered as
        colour, order and a word. Bulk actions run through
        `ui/src/domain/bulkAction.ts`: six requests in flight rather than one per
        selected row, one summary notification rather than forty, and whatever
        failed stays selected so it can be retried. Claiming is a race the engine
        allows, so a partial failure is normal and had to be reportable.
  - [ ] Medium-priority UX items (5-8) delivered.
  - [ ] Lower-priority UX items (9-12) delivered.
- [ ] 8. 90-Day Execution Plan
  - [ ] Phase 1 complete (`baseline profiling + SLO dashboard`, `top 5 bottlenecks`, `critical security gaps`).
  - [ ] Phase 2 complete (`architecture cleanup`, `high-value UX improvements`).
  - [ ] Phase 3 complete (`load/chaos`, `canary + hardening`, `playbooks/docs`).

#### 10. Session Execution Log

- This session should prioritize concrete, verifiable roadmap steps over broad rewrites.
- Any completed recommendation must include code/config changes or explicit verification evidence.
- 2026-03-24 (completed): Executed P0 Security/ Reliability CI baseline.
  - Added `.github/workflows/security_reliability_ci.yml`.
  - Coverage now includes:
    - SAST baseline: `go vet` on `./cmd/gobpm ./internal/app ./server/interceptors/...`.
    - Reliability baseline: `go test -race` on `./internal/app ./server/interceptors/...`.
    - Build gate: `go build ./cmd/gobpm`.
    - Dependency vulnerability scan: `govulncheck`.
    - Secret scanning: `gitleaks` action.
- 2026-03-24 (completed): Executed P0 secret/PII redaction hardening and CI scope expansion.
  - Added centralized sanitizer: `internal/pkg/redaction/redactor.go` (+ unit tests).
  - Redaction integrated in shared transport error serialization:
    - `server/transports/https/common/utils.go`
    - `server/transports/grpcs/common/utils.go`
  - Redaction integrated in setup test-connection outward error messages:
    - `server/domains/services/impl/setup.go`
  - Startup logging hardening for dynamic values:
    - `internal/app/app.go`
  - Expanded CI coverage in `.github/workflows/security_reliability_ci.yml`:
    - `go vet`, `go test -race`, and `govulncheck` now include `./internal/pkg/redaction`, `./server/transports/grpcs/common`, and `./server/transports/https/common`.
  - Verification evidence:
    - `go test ./internal/pkg/redaction ./server/transports/grpcs/common ./server/transports/https/common ./server/domains/services/impl ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
- 2026-03-24 (completed): Executed P1 profiling baseline (`pprof`) with guarded runtime exposure and measured hotspots.
  - Added guarded profiling server in `internal/app/app.go`:
    - Enabled only when `GOBPM_PPROF_ENABLED=true`.
    - Configurable address via `GOBPM_PPROF_ADDRESS` (default `127.0.0.1:6060`).
    - Lifecycle-managed startup/shutdown using existing app errgroup and timeout pattern.
  - Added focused tests in `internal/app/profiling_test.go`:
    - Env parsing and default address resolution.
    - `pprof` handler route availability.
  - Runtime baseline workload used for measurement:
    - Repeated requests to `GET /api/v1/setup/status` while collecting `pprof` CPU (`20s`) and heap snapshots.
  - Top 5 baseline hotspots and targets:
    - CPU (flat): `runtime.cgocall` (`57.75%`) → target `< 40%` by reducing per-request socket churn and improving keep-alive reuse.
    - CPU (cum): `net/http.(*conn).serve` (`59.69%`) → target `< 45%` by trimming request-path overhead on high-frequency endpoints.
    - CPU (cum): `github.com/gsoultan/gobpm/internal/app.(*App).runServers.func2` (`23.64%`) → target `< 15%` via handler/interceptor allocation reduction.
    - Heap (flat): `runtime.mallocgc` (`30.43%`) → target `< 22%` by removing avoidable transient allocations.
    - Heap (flat): `regexp.compile` (`12.15%`) → target `< 3%` by precompiling/caching regex construction.
  - Verification evidence:
    - `go test ./internal/app`
    - `go build ./cmd/gobpm`
    - `go tool pprof -top http://127.0.0.1:6060/debug/pprof/profile?seconds=20`
    - `go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap`
- 2026-03-24 (completed): Executed P1 reproducible load/profiling harness and concrete optimization backlog definition.
  - Added reproducible script: `tests/performance/setup_status_profile.ps1`.
  - Script behavior:
    - Resolves `ENCRYPTION_KEY` from `config.yaml` and applies it for deterministic startup (falls back to pre-set env var only if config key is unavailable).
    - Starts `gobpm` with guarded `pprof` env flags.
    - Executes warm-up + sustained `GET /api/v1/setup/status` load.
    - Captures CPU and heap `pprof` outputs to timestamped files under `tests/performance/artifacts`.
  - Added checklist governance in this roadmap with explicit `[x]/[ ]` status per roadmap area.
  - Converted hotspot targets into concrete P1 optimization backlog items:
    - `P1-OPT-01` (Owner: Backend, Est: S): setup-status request-path overhead trim; guardrail: no API behavior changes; rollback: revert endpoint/interceptor micro-optimizations.
    - `P1-OPT-02` (Owner: Backend, Est: M): connection churn reduction and keep-alive behavior verification under load; guardrail: no long-lived leaked conns; rollback: disable tuning and restore previous transport settings.
    - `P1-OPT-03` (Owner: Backend, Est: S): regex compile hotspot audit and caching/precompile fixes where dynamic compile is found; guardrail: no redaction coverage regression; rollback: revert specific regex-path patch.
  - Verification evidence:
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 100 -LoadRequests 1200 -CPUProfileSeconds 10`
    - `tests/performance/artifacts/setup-status-cpu-20260324-192646.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-192646.txt`
- 2026-03-24 (completed): Executed `P1-OPT-01` setup-status request-path overhead trim and re-profiled.
  - Hot-path optimization implemented:
    - `server/transports/https/http.go`: pre-wrap authentication middleware once (`authenticatedHandler := authMiddleware.Wrap(m)`) and reuse it per request instead of re-wrapping on every request.
  - Profiling harness reliability fix:
    - `tests/performance/setup_status_profile.ps1`: deterministic `ENCRYPTION_KEY` resolution from `config.yaml` to avoid stale-env startup failures.
  - Verification evidence:
    - `go test ./server/transports/https/... ./internal/app`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-193320.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-193320.txt`
  - Result notes:
    - Post-change CPU profile captured non-zero samples and shows request-path activity concentrated in `net/http` + interceptor stack with no behavioral regressions.
    - Prior run artifact `setup-status-cpu-20260324-192646.txt` had zero samples, so direct numeric delta against that file is non-authoritative; baseline hotspot targets remain tracked from earlier documented `P1` profile entry.
- 2026-03-24 (completed): Executed `P1-OPT-02` connection-churn guardrails and keep-alive verification under sustained load.
  - Connection-behavior tuning implemented:
    - `internal/app/app.go`: added `newHTTPServer` and applied it to both HTTP and pprof servers with explicit `ReadHeaderTimeout`, `IdleTimeout`, and `MaxHeaderBytes` settings to harden and stabilize keep-alive behavior.
  - Regression guard coverage added:
    - `internal/app/profiling_test.go`: added `TestNewHTTPServer` to lock server configuration values.
  - Verification evidence:
    - `go test ./internal/app`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-193920.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-193920.txt`
  - Result notes:
    - Sustained-load profiling completed successfully with no startup/transport regressions; request-path CPU remains dominated by socket I/O (`runtime.cgocall`/`net/http`), preserving the measured baseline for the next optimization pass.
- 2026-03-24 (completed): Executed `P1-OPT-03` regex compile hotspot audit and lazy compile/cache optimization.
  - Hotspot audit result:
    - Project regex compilation usage is concentrated in `internal/pkg/redaction/redactor.go`; no per-request dynamic `regexp.Compile` loops were found.
  - Optimization implemented:
    - `internal/pkg/redaction/redactor.go`: moved regex creation behind `sync.Once` (`getPatterns`) so compilation is lazy and cached on first use instead of eager package initialization.
  - Regression coverage added:
    - `internal/pkg/redaction/redactor_test.go`: added cache reuse + concurrent-call tests for `getPatterns` while preserving existing redaction behavior tests.
  - Verification evidence:
    - `go test ./internal/pkg/redaction`
    - `go test ./server/transports/grpcs/common ./server/transports/https/common ./server/domains/services/impl ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-194919.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-194919.txt`
  - Result notes:
    - `regexp.compile` dropped out of the latest setup-status heap top output (`setup-status-heap-20260324-194919.txt`), indicating the targeted hotspot was removed from this workload path.
- 2026-03-24 (completed): Executed `P1-OPT-04` setup-status rate-limit transient-overhead reduction and sustained-load re-profile.
  - Optimization implemented:
    - `server/interceptors/security/rate_limit_interceptor.go`: changed `windows` to `map[string]*clientRequestWindow` and updated existing windows in place, eliminating per-request map reassign for active clients.
    - `server/interceptors/security/rate_limit_interceptor.go`: simplified `clientKeyFromRequest` fast path and added `hostFromRemoteAddr` host extraction helper to reduce request-path parsing overhead.
  - Regression coverage added:
    - `server/interceptors/security/rate_limit_interceptor_test.go`: added window-entry reuse test and extended client key extraction coverage (IPv6 bracketed, no-port fallback).
  - Verification evidence:
    - `go test ./server/interceptors/security`
    - `go test ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-200816.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-200816.txt`
  - Result notes:
    - In this profile sample, setup-status hot-path shares declined for rate-limit frames (`Wrap.func1` from `~6.45%` to `~5.56%`, `clientKeyFromRequest` from `~1.08%` to `~0.51%`).

- 2026-03-24 (completed): Executed `P1-OPT-05` setup-status auth public-path lookup optimization and sustained-load re-profile.
  - Optimization implemented:
    - `server/interceptors/auth/interceptor.go`: replaced per-request linear public-path scan in `mandatoryHTTPAuthInterceptor` with precomputed `map[string]struct{}` lookup.
    - `server/interceptors/auth/interceptor.go`: added `bearerTokenFromHeader` using `strings.Cut` and whitespace validation to avoid split-slice allocation and enforce stricter bearer-token parsing.
  - Regression coverage added:
    - `server/interceptors/auth/interceptor_test.go`: new table-driven tests for bearer header parsing and mandatory auth behavior (public-path bypass, protected-path enforcement, invalid header rejection, failed auth, and context injection on success).
  - Verification evidence:
    - `go test ./server/interceptors/auth ./server/interceptors/security ./server/interceptors/... ./internal/app`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-201633.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-201633.txt`
  - Result notes:
    - In this sample, request-path interceptor overhead remained reduced (`rateLimitInterceptor.Wrap.func1` cumulative share improved from `~5.56%` to `~4.21%`) with no behavioral regressions detected by targeted tests.

- 2026-03-24 (completed): Executed `P1-OPT-06` setup-status optional-auth transient-allocation reduction and sustained-load re-profile.
  - Optimization implemented:
    - `server/interceptors/auth/interceptor.go`: updated optional `httpAuthInterceptor` to reuse `bearerTokenFromHeader` (`strings.Cut`) instead of per-request `strings.Split`, removing split-slice allocation on authenticated requests while preserving pass-through behavior for missing/invalid headers.
  - Regression coverage added:
    - `server/interceptors/auth/interceptor_test.go`: added table-driven optional-auth tests for no-header pass-through, invalid-header pass-through, failed-auth pass-through, and successful-auth context injection.
  - Verification evidence:
    - `go test ./server/interceptors/auth ./server/interceptors/security ./server/interceptors/... ./internal/app`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-202613.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-202613.txt`
  - Result notes:
    - Optional-auth path no longer allocates from header splitting in this interceptor path; targeted tests confirm unchanged request authorization semantics.

- 2026-03-24 (completed): Executed `P1-OPT-07` slice preallocation pass for high-frequency gRPC list-response mapping.
  - Optimization implemented:
    - `server/transports/grpcs/definitions/server.go`: preallocated `defs` in `encodeGRPCListDefinitionsResponse` with `len(resp.Definitions)`.
    - `server/transports/grpcs/organizations/server.go`: preallocated `orgs` in `encodeGRPCListOrganizationsResponse` with `len(resp.Organizations)`.
    - `server/transports/grpcs/projects/server.go`: preallocated `projects` in `encodeGRPCListProjectsResponse` with `len(resp.Projects)`.
    - `server/transports/grpcs/processes/server.go`: preallocated `instances` in `encodeGRPCListInstancesResponse` with `len(resp.Instances)`.
    - `server/transports/grpcs/tasks/server.go`: preallocated `tasks` in `encodeGRPCListTasksResponse` with `len(resp.Tasks)`.
  - Guardrails:
    - Kept existing `nil` behavior for empty lists by only allocating when source slice length is greater than zero.
  - Verification evidence:
    - `go test ./server/transports/grpcs/definitions ./server/transports/grpcs/organizations ./server/transports/grpcs/projects ./server/transports/grpcs/processes ./server/transports/grpcs/tasks`
    - `go test ./server/transports/grpcs/common ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-203816.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-203816.txt`
  - Result notes:
    - Eliminated repeated growth allocations in list-response conversion paths by reserving known capacity up front; no behavior regressions observed in verification scope.

- 2026-03-24 (completed): Executed `P1-OPT-08` request-path logging/serialization optimization and sustained-load re-profile.
  - Optimization implemented:
    - `server/interceptors/logging/interceptor.go`: switched `took` emission from `time.Since(begin).String()` to typed `Dur("took", time.Since(begin))` serialization to avoid per-request duration string conversion.
    - `server/interceptors/logging/interceptor.go`: removed duplicate `Failed()` evaluation by reading failer error once and reusing the result.
  - Regression coverage added:
    - `server/interceptors/logging/interceptor_test.go`: added `failer called once` test to verify single `Failed()` invocation and preserve endpoint behavior.
  - Verification evidence:
    - `go test ./server/interceptors/logging ./server/interceptors/auth ./server/interceptors/security ./server/interceptors/... ./internal/app`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-214922.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-214922.txt`
  - Result notes:
    - In this sample, `time.Time.appendFormat` cumulative share on the setup-status profile path reduced from `~3.60%` (`setup-status-cpu-20260324-203816.txt`) to `~2.62%` (`setup-status-cpu-20260324-214922.txt`) while preserving request behavior in test scope.

- 2026-03-24 (completed): Executed `P1-OPT-09` backpressure guardrail implementation and sustained-load re-profile.
  - Optimization implemented:
    - `server/interceptors/security/backpressure_interceptor.go`: added bounded queue + bounded in-flight worker limiter with overload rejection (`503` + `Retry-After`) and queued-request cancellation handling (`408` on context timeout/cancel before execution).
    - `server/interceptors/factory.go`: added `NewBackpressure(maxInFlightRequests, maxQueuedRequests)` factory method.
    - `internal/app/app.go`: wired backpressure into HTTP interceptor chain with conservative defaults (`max in-flight=128`, `max queued=256`) before rate limiting and auth for earlier saturation shedding.
  - Regression coverage added:
    - `server/interceptors/security/backpressure_interceptor_test.go`: added table-driven constructor/default tests and behavioral tests for normal pass-through, queue overflow rejection, queued wait-until-slot-free flow, and context-cancel while queued.
  - Verification evidence:
    - `go test ./server/interceptors/security ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-222258.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-222258.txt`
  - Result notes:
    - Sustained-load profile completed successfully after adding queue/worker bounds, preserving endpoint behavior and adding saturation protection for the high-frequency setup-status path.

- 2026-03-24 (completed): Executed `P1-OPT-10` heavy-traffic idempotency key support for externally triggered HTTP write commands.
  - Optimization implemented:
    - `server/interceptors/security/idempotency_interceptor.go`: added `Idempotency-Key` based request deduplication for mutating HTTP methods with in-memory TTL cache, request-hash conflict detection (`409`), queued replay for in-flight duplicates, and replay marker header (`Idempotency-Replayed: true`).
    - `server/interceptors/factory.go`: added `NewIdempotency(ttl time.Duration)` factory method.
    - `internal/app/app.go`: wired idempotency interceptor into the HTTP middleware chain after mandatory auth and before endpoint handlers with conservative default TTL (`15m`).
    - `server/transports/https/http.go`: added `Idempotency-Key` to CORS allowed request headers.
  - Regression coverage added:
    - `server/interceptors/security/idempotency_interceptor_test.go`: added table-driven constructor coverage and behavioral tests for pass-through without key, replay on duplicate requests, key-reuse conflict on mismatched payload, and cancellation while waiting on an in-flight request.
  - Verification evidence:
    - `go test ./server/interceptors/security ./internal/app ./server/interceptors/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260324-223508.txt`
    - `tests/performance/artifacts/setup-status-heap-20260324-223508.txt`
  - Result notes:
    - Duplicate write-command retries with the same key now return the original cached response without re-executing handlers, while key reuse with a different payload is explicitly rejected to preserve correctness under client retries and burst traffic.

- 2026-03-25 (completed): Executed `P1-OPT-11` heavy-traffic retry policy with exponential backoff + jitter for externally triggered inbound command dispatch.
  - Optimization implemented:
    - `server/domains/services/impl/messaging.go`: switched `messagingService` engine dependency to `contracts.EngineEventBus` (consumer-centric dispatch boundary).
    - `server/domains/services/impl/messaging.go`: added bounded retry for inbound `SendMessage` dispatch (`max attempts=3`) with exponential backoff, bounded jitter, and context-aware wait/cancel handling.
    - `server/domains/services/impl/messaging.go`: added non-retry classification for `context.Canceled` / `context.DeadlineExceeded` errors to avoid wasteful retries after cancellation/timeout.
  - Regression coverage added:
    - `server/domains/services/impl/messaging_test.go`: added table-driven coverage for first-attempt success, success-after-retry, terminal failure after max attempts, non-retryable cancellation errors, and cancellation while waiting for retry.
    - `server/domains/services/impl/messaging_test.go`: added retry-delay cap coverage (`max backoff + max jitter`).
  - Verification evidence:
    - `go test ./server/domains/services/impl`
    - `go test ./server/domains/services/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260325-001134.txt`
    - `tests/performance/artifacts/setup-status-heap-20260325-001134.txt`
  - Result notes:
    - Inbound external message dispatch now uses bounded retries with jitter under transient failures while preserving fast-fail behavior for canceled/timed-out contexts.

- 2026-03-25 (completed): Executed `P1-OPT-12` heavy-traffic DLQ handling for poison inbound messaging flows.
  - Optimization implemented:
    - `server/domains/services/impl/messaging.go`: added inbound DLQ queue bootstrap (`<queue>.dlq`) in consumer setup.
    - `server/domains/services/impl/messaging.go`: refactored inbound delivery processing to route poison messages to DLQ for JSON unmarshal failures and terminal dispatch failures after retry exhaustion.
    - `server/domains/services/impl/messaging.go`: added structured DLQ payload publication (original queue, message name, correlation key, failure reason/error, timestamp, original payload/raw body) with bounded publish timeout.
    - `server/domains/services/impl/messaging.go`: preserved non-DLQ behavior for cancellation/deadline errors to avoid false poison routing during shutdown/timeout conditions.
  - Regression coverage added:
    - `server/domains/services/impl/messaging_test.go`: added table-driven tests for DLQ skip on successful dispatch, DLQ routing after retry exhaustion, DLQ routing on unmarshal failures, joined error behavior when DLQ publish fails, and no-DLQ handling for context-canceled dispatch.
  - Verification evidence:
    - `go test ./server/domains/services/impl ./server/domains/services/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260325-002021.txt`
    - `tests/performance/artifacts/setup-status-heap-20260325-002021.txt`
  - Result notes:
    - Poison inbound messages are now persisted to a dedicated DLQ for operational recovery while retaining retry behavior and cancellation-aware fast-fail semantics.

- 2026-03-25 (completed): Executed `P1-OPT-13` heavy-traffic partition-by-key execution for inbound messaging dispatch.
  - Optimization implemented:
    - `server/domains/services/impl/inbound_partition_executor.go`: added a bounded partition executor with deterministic key-to-partition routing and context-aware lifecycle stop handling.
    - `server/domains/services/impl/messaging.go`: routed inbound dispatch through `dispatchInboundMessage` using `correlation_key` partitioning while preserving retry/DLQ behavior.
    - `server/domains/services/impl/messaging.go`: integrated executor shutdown into `StopAll` to keep service lifecycle bounded.
  - Regression coverage added:
    - `server/domains/services/impl/inbound_partition_executor_test.go`: added tests for validation errors, same-key serialization, cross-partition parallelism, and queued-task context cancellation.
  - Verification evidence:
    - `go test ./server/domains/services/impl -run InboundPartition -v -count=1 -timeout 60s`
    - `go test ./server/domains/services/impl ./server/domains/services/...`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260325-004204.txt`
    - `tests/performance/artifacts/setup-status-heap-20260325-004204.txt`
  - Result notes:
    - Inbound dispatch now preserves ordered execution for the same correlation key while allowing different keys to proceed in parallel under bounded worker/queue limits.

- 2026-03-25 (completed): Executed `P2-UX-01` Business Timeline narrative audit log.
  - Implemented:
    - `server/domains/services/contracts/audit_writer.go`: `AuditWriter` service contract (`RecordEvent`).
    - `server/domains/services/impl/audit_writer.go`: `auditWriter` implementation with `narrativeFor` pure function covering 12 event types with plain-English sentences (e.g. `alice claimed task "Review Invoice"`). ISP-correct: depends only on `repcontracts.AuditRepository`.
    - `server/domains/services/impl/task.go`: `recordAuditEvent` helper + wired into `ClaimTask`, `UnclaimTask`, `DelegateTask`, `CompleteTask`, `AssignTask`, `CreateTaskForNode`.
    - `server/domains/services/service.go`: `NewAuditWriter(repo.Audit())` injected into `NewTaskService`.
  - Tests: `server/domains/services/impl/audit_writer_test.go` — 14 `narrativeFor` table cases + 5 `actorName`/`subjectName` cases + 3 `RecordEvent` behavior tests (enrichment, preserve custom, repo error).
  - Verification evidence:
    - `go test ./server/domains/services/impl -run "TestNarrative|TestActorName|TestSubjectName|TestRecordEvent" -v` — all 22 cases pass.
    - `go test ./server/domains/services/impl/... ./server/interceptors/... ./internal/app`
    - `go build ./cmd/gobpm`

- 2026-03-25 (completed): Executed `P0-SEC-01` RBAC/ABAC enforcement and tenant-isolation hardening.
  - Security implemented:
    - `server/interceptors/auth/rbac.go`: `AccessPolicy` ABAC interface, `allowAllPolicy` Null Object, `rbacInterceptor` with `NewRBACInterceptor` and `NewRequireRoles` convenience factory.
    - `rolesFromContext` supports both `entities.User` and `auth.UserClaims` (JWT + OIDC strategies).
    - `server/repositories/gorms/tenant.go`: `tenantScopeDB` helper reads `TenantContext` from context and applies `JOIN projects ON projects.id = {table}.project_id AND projects.organization_id = ?`.
    - `server/repositories/gorms/queries.go`: added `QueryTenantScopeViaProject` constant.
    - `server/repositories/gorms/process.go`: `List()` now tenant-scoped.
    - `server/repositories/gorms/task.go`: `List()`, `ListByAssignee()`, `ListByCandidates()` now tenant-scoped.
  - DB parameterization audit: clean - no SQL injection vectors found.
  - Regression coverage: `server/interceptors/auth/rbac_test.go` with 8 table-driven cases + `TestNewRequireRoles_PassesNextOnMatch` + `TestAllowAllPolicy`.
  - Verification evidence:
    - `go test ./server/interceptors/auth/...`
    - `go build ./server/repositories/...`
    - `go build ./cmd/gobpm`

- 2026-03-25 (completed): Executed `P1-OPT-14` heavy-traffic context timeout/cancellation propagation audit and bounded dispatch hardening.
  - Optimization implemented:
    - `server/domains/services/impl/messaging.go`: added explicit per-inbound-dispatch timeout budget (`10s` default) via `context.WithTimeoutCause` in `dispatchInboundMessage`.
    - `server/domains/services/impl/messaging.go`: applied bounded dispatch context consistently for both direct dispatch and partition-executor dispatch paths.
    - `server/domains/services/impl/messaging.go`: replaced reconnect retry `time.After` wait with context-aware `sleepWithContext` to keep consumer retry loop cancellation-safe and timer-bounded.
  - Regression coverage added:
    - `server/domains/services/impl/messaging_test.go`: added timeout behavior tests for dispatch with and without partition executor.
    - `server/domains/services/impl/messaging_test.go`: added timeout path verification that dispatch deadline errors are not routed to DLQ.
  - Verification evidence:
    - `go test ./server/domains/services/impl -run Messaging -count=1`
    - `go test ./server/domains/services/... -count=1`
    - `go build ./cmd/gobpm`
    - `powershell -ExecutionPolicy Bypass -File .\tests\performance\setup_status_profile.ps1 -WarmupRequests 200 -LoadRequests 10000 -CPUProfileSeconds 15`
    - `tests/performance/artifacts/setup-status-cpu-20260325-005612.txt`
    - `tests/performance/artifacts/setup-status-heap-20260325-005612.txt`
  - Result notes:
    - Inbound message dispatch now has an explicit upper-bound execution budget and preserves existing retry/DLQ semantics, reducing risk of unbounded blocked dispatch under heavy traffic.

- 2026-08-16 (completed): Closed the `P0-SEC-02` tenant-scoping coverage gap on read paths.
  - Scope extended from 4 tables to every project-owned table:
    - `server/repositories/gorms/audit.go`, `form.go`, `deployment.go`,
      `external_task.go`, `subscription.go`, `connector.go`, `notification.go`.
    - `server/repositories/gorms/tenant.go`: added `tenantScopeDBOptionalProject`
      (null-tolerant, for `notifications`) and `tenantScopeDeploymentResources`
      (scopes through the parent deployment, which is where the project lives).
    - `server/repositories/gorms/queries.go`: added the two new JOIN clauses and
      `QualifiedByID`, because a bare `id = ?` is ambiguous once projects is joined.
  - `Get`-by-ID reads are scoped too, not only lists: holding another organization's
    UUID now reads as `ErrRecordNotFound` instead of returning the row. That closed an
    IDOR on `connector_instances`, which stores configured credentials.
  - Deliberately unscoped, with comments saying why: `FetchAndLock` (worker long-poll,
    runs `SELECT ... FOR UPDATE`) and `ListTemplatedMessageSubscriptions` (installation-
    wide background sweep with no request context).
  - Regression coverage: `tests/tenant/isolation_test.go` — 4 tests × 3 SQL dialects,
    covering cross-tenant lists, cross-tenant `Get`, own-rows-still-readable (so an
    over-broad join cannot pass), and the documented no-context fail-open behaviour.
    17 of 18 subtests fail without the fix.
  - `tests/testutils/db.go` now migrates from the shared `migrationModels()` list rather
    than a second copy, and that list gained `FormModel`, `NotificationModel`,
    `DeploymentModel` and `ResourceModel`, which no test database had before.
  - Verification evidence:
    - `go build ./...`, `go vet ./...` — both green
    - `go test ./...` and `go test -race ./...` — green module-wide
    - `go test ./tests/tenant/ -v` against live PostgreSQL 17 and MySQL 8 — all pass
    - `bunx tsc -b --force`, `bun run lint`, `bun run build`, `bun run test` — green

- 2026-08-16 (completed): Closed `P0-SEC-03` write-path tenant scoping, and the by-ID reads
  that `P0-SEC-02` had missed.
  - **What the earlier pass got wrong:** `tasks`, `process_instances`,
    `process_definitions` and `decision_definitions` were recorded as tenant-scoped, but
    only their list queries were. Every `Get`-by-ID on them was open — a UUID was enough to
    read another organization's task, running instance, deployed BPMN XML or decision
    table. `GetByKey`/`GetByKeyAndVersion` were both a leak and a wrong answer, since keys
    are unique per project and the lookup searched globally.
  - Reads now scoped: `Get`, `GetForUpdate`, `GetByKey`, `GetByKeyAndVersion` across
    `task.go`, `process.go`, `definition.go`, `decision.go`. `GetForUpdate` uses the
    subquery form so `FOR UPDATE` does not lock `projects` as well.
  - Writes now scoped: `Update`, `UpdateStatus`, `Delete`, `MarkAsRead`, `MarkAllAsRead`,
    `DeleteByNode` and `UpdateCorrelationKey` across nine repositories.
  - **Why writes use a guard, not a scoped statement:** GORM's `Save` ignores a preceding
    `Where` — verified on SQLite, PostgreSQL and MySQL, where the row updated anyway. A
    scope written that way would read as applied and enforce nothing. Rewriting `Save` as
    `Model().Where().Updates()` was rejected because `Updates` skips zero values, so
    clearing a field would quietly stop persisting. `requireVisibleToTenant` therefore does
    a scoped existence check and returns `ErrRecordNotFound`; it is a no-op when there is
    no tenant context, so background work pays nothing.
  - Regression coverage: `tests/tenant/isolation_test.go` grew to 198 subtests across three
    dialects. The write cases assert both that the call is refused **and** that the target
    row is unchanged — an error return alone would not have caught the `Save` trap.
  - Verification evidence:
    - `go build ./...`, `go vet ./...` — green
    - `go test ./...`, `go test -race ./...` — green module-wide with live PostgreSQL 17
      and MySQL 8
    - `go test ./tests/tenant/ -count=1` — 198 subtests, all pass

- 2026-08-16 (completed): Executed `P0-OPS-01` operability baseline and `P0-SEC-04`
  create-path scoping, and added the first fuzz targets.
  - **Health and readiness** (`internal/pkg/health`): `/healthz` checks nothing external,
    because a liveness probe that consulted the database would fail on every replica at
    once during a database blip and have the orchestrator restart the whole fleet.
    `/readyz` does check it, so a replica that cannot serve is pulled from the load
    balancer. Both wrap outside every interceptor: probes carry no credentials, and a
    probe shed by the backpressure limiter reports a busy process as a dead one.
  - **Metrics** (`internal/pkg/metrics`): the SLOs in §1 had nothing measuring them.
    Histogram buckets sit *on* the 150ms and 500ms thresholds, since a quantile
    interpolated across a bucket spanning the target cannot say which side it is on. The
    route label is bounded at 200 values with an overflow bucket — it derives from an
    attacker-supplied path, and an unbounded label is an unbounded map keyed by remote
    input. Scrape endpoint on loopback `:9464`, separate from the public API.
  - **Fuzz targets** (`tests/fuzz`): five, covering the BPMN XML parser, its round trip,
    the condition evaluator chain and the FEEL evaluator. Two real defects found:
    - **Script sandbox DoS.** `new Array(1e9).join('x')` as a gateway condition ran for
      **37.6s against a 200ms budget** — goja honours interrupts only between statements
      and cannot pre-empt a single native call. Every token through such a gateway held a
      job worker for the duration; enough of them stop the engine, which is the exact
      denial of service the budget existed to prevent. `RunSandboxed` now runs the script
      on its own goroutine and releases the caller after the budget plus a grace period,
      returning `ErrScriptAbandoned`. Measured after: 0.70s. This bounds worker
      starvation, **not memory** — the abandoned script keeps allocating, and goja offers
      no heap limit. The real fix is Phase 2.2, which takes JavaScript off gateway
      conditions by default.
    - **`Parse` returned `(nil, nil)`** for BPMN with no `<process>` — reachable by
      uploading a file exported with only a collaboration or pool. It did not crash only
      because `Accept` had been hardened separately; it was a landmine for any new caller
      and surfaced to the user as a validation error about a definition they never wrote.
      Now returns `ErrNoProcessInDefinition`, whose message says what to fix.
  - **Create-path and project scoping**: `requireProjectInTenant` refuses a create
    pointed at another organization's project. `projects` itself is now scoped — it was
    the one table nothing covered, because it is what every other scope joins *through*,
    so `List()` had been returning every organization's projects to anyone authenticated.
  - Verification evidence:
    - `go build ./...`, `go vet ./...` — green
    - `go test ./...`, `go test -race ./...` — green module-wide with live PostgreSQL 17
      and MySQL 8
    - `bunx tsc -b --force`, `bun run lint`, `bun run build`, `bun run test` — green
    - Probes and metrics verified against a running server, not only unit tests:
      `/healthz` and `/readyz` return 200, `/api/v1/tasks` still returns 401, the public
      port does not serve metrics, and the 401 is recorded as `status_class="4xx"`
    - Fuzzers run beyond their seeds: 4.1M executions on the parser after the fix, clean

- 2026-08-16 (completed): Executed `P0-OPS-02` versioned schema migrations, replacing
  AutoMigrate-on-every-boot.
  - `server/repositories/migrations`: a `schema_migrations` table records every applied
    version with its duration, so "which version is this database at" and "which migration
    was slow" both have answers, which AutoMigrate could never give.
  - Migration 1 is the baseline AutoMigrate over `MigrationModels()`. It reproduces exactly
    the schema every existing installation already has, so it is a no-op on all of them
    and creates everything on a fresh database — that is what lets versioning start
    without a migration that has to guess at the current state.
  - Migrations are Go, not SQL: four supported dialects would otherwise mean four copies
    of every change, with the differences discovered in production.
  - **The two data backfills became migrations 2 and 3.** They had been running on *every
    boot*, and `BackfillEngineBookkeeping` calls `Process().List` — every process instance
    ever created, loaded into memory — so startup got slower forever while finding nothing
    to do after the first run. Verified: first boot `applied=[1,2,3]`, second boot
    `alreadyApplied=3 applied=[]`.
  - Replicas start together, so they contend for a lock row (`schema_migration_locks`).
    Advisory locks differ across all four engines; a primary key that can only be inserted
    once behaves the same everywhere. Stale locks are taken over after 15 minutes, or one
    replica crashing mid-migration would block every future deployment.
  - `DriftReport` warns at startup when a model declares a column the database lacks. This
    is what makes strict migrations usable: adding a model field no longer applies itself,
    and forgetting the migration would otherwise fail at the first request that touches
    the column rather than at boot.
  - Setup runs the same schema migrations rather than a bare AutoMigrate, so a freshly
    created database is not treated on its first boot as one that had never been migrated.
  - **Two real defects found by the concurrency test**, both of which would have hit a
    multi-replica deployment:
    - `AutoMigrate` is not concurrency-safe: replicas racing to create the bookkeeping
      tables failed with "table already exists". Now tolerated, but only after confirming
      the table really is there, so a genuine failure still stops the deployment.
    - The lock could report a hard error where it should have retried — a holder that
      finished quickly released the row between the failed insert and the check, making
      "no lock held" indistinguishable from contention. Every failure is now retried under
      a deadline, with the last error reported if it expires.
    - Also fixed: GORM had made `version` an `AUTOINCREMENT` column. It is an identity we
      assign, and a database that renumbered it would lose track of which migration is
      which.
  - Regression coverage: `tests/migrations/runner_test.go` — 8 tests across SQLite,
    PostgreSQL and MySQL, covering apply-exactly-once, upgrade from an existing version,
    stop-at-first-failure without recording it, four concurrent replicas applying a
    migration exactly once, duplicate version rejection, ordering, baseline idempotency
    against an already-migrated database, and drift detection.
  - Verification evidence:
    - `go build ./...`, `go vet ./...` — green
    - `go test ./...`, `go test -race ./...` — green module-wide with live PostgreSQL 17
      and MySQL 8
    - `bunx tsc -b --force`, `bun run lint`, `bun run build`, `bun run test` — green
    - Two consecutive real boots, showing the migrations run once and then not again

- 2026-08-16 (completed): Closed the last open P0 items — authorization, tracing and recovery.
  - **`P0-SEC-05` two authorization holes**, both found while closing the fail-open scope:
    - The tenant resolver passed an *unresolvable* principal through with no
      `TenantContext`, which the repository layer reads as a system call and does not
      scope. OIDC validation returns `*auth.UserClaims`, which carries no membership list,
      so every OIDC-authenticated request reached every tenant's rows. Now refused: the
      three cases (no principal, resolved, unresolvable) are distinct, and collapsing the
      last into the first was the bug.
    - `CreateUser` sat on the public endpoint chain — logging only, no role check, no
      tenant resolution — while `UpdateUser` and `DeleteUser` beside it are `adminOnly`.
      Mandatory HTTP auth meant a token was needed, but any authenticated caller at any
      privilege level could post a user with `roles:["admin"]` and someone else's
      organization, then log in as their administrator. Now `adminOnly`, and the named
      organizations are checked against the caller's tenant, because being an admin grants
      authority over your own organization rather than every organization.
    - The wiring test now asserts nothing administrative sits on the public chain, and
      that the only public endpoints are those reachable before a caller can hold a token.
  - **`P0-OPS-03` tracing** (`internal/pkg/tracing`): OTLP, off unless an endpoint is
    configured, no-op and free when off. Spans on the job service task (instance, node,
    definition, attempt) and on the connector call (key, status, latency), which is what
    §3.4 asks for — the connector span is the boundary where this system stops being in
    control, and usually the answer to "what is this instance stuck on". Sampling defaults
    to 5%, not 100%: an engine executing thousands of nodes a minute would otherwise make
    the trace exporter its own outage. `ParentBased` keeps traces whole rather than holed.
  - **`P0-OPS-04` recovery** ([`docs/recovery.md`](../docs/recovery.md)): production RPO
    5min / RTO 1h, staging 24h/4h, with the reasoning. The 5-minute RPO is chosen against
    connector idempotency keys — they are what make a non-zero RPO survivable, since a
    retried outbound call after recovery must not double-charge. Documents that
    `ENCRYPTION_KEY` must be backed up *separately* (a database backup without it restores
    unreadable rows), what a restore actually does to running instances (timer stampede,
    re-sent external calls, human tasks completed twice), and a quarterly rehearsal —
    because a restore nobody has performed is a hypothesis. It is also the migration
    rollback plan, the runner being forward-only.
  - Verification evidence:
    - `go build ./...`, `go vet ./...` — green
    - `go test ./...`, `go test -race ./...` — green module-wide with live PostgreSQL 17
      and MySQL 8
    - `bunx tsc -b --force`, `bun run lint`, `bun run build`, `bun run test` — green

- 2026-08-17 (completed): Executed `P2-INT-01` — the integration surface: HTTP worker
  protocol, Go client SDK, and per-node API guidance.
  - **External-task worker protocol over HTTP** (`server/transports/https/external_tasks`):
    fetch-and-lock, complete, failure. Previously gRPC/AMQP only, so a worker in "anything
    that speaks HTTP" was impossible. Durations are `_ms`-suffixed on the wire because a
    bare `lock_duration` was already misread once: the AMQP bridge passed 30 *seconds* to a
    repository reading *milliseconds*, so bridge locks expired after 30ms and every poll
    re-fetched the same tasks. Fixed.
  - **`ImportDefinition` requires a project** — an imported definition had none, and under
    tenant scoping was deployed, versioned, and permanently invisible to its own
    organization. The XML parser now also carries `topic=` / `camunda:topic` into
    `ExternalTopic` both ways, so XML-deployed processes can produce external tasks at all.
  - **Go SDK** (`sdk/`, own module, zero dependencies): deploy/start, messages/signals,
    tasks, and a long-poll `Worker` whose handler budget is its lock. Proven by
    `sdk/examples/quickstart` against a live server: login → deploy BPMN → worker serves
    the external task → human task claimed/completed → instance completed → timeline read.
    `docs/integration.md` documents exactly what that program exercises. CI and `make gate`
    include the SDK module, which the module-wide commands cannot see.
  - **Transaction-joining sweep, found by running the product**: five repositories (29 call
    sites — variable snapshots, connectors, external tasks, incidents, compensatable
    activities) called `ResolveDB` instead of `GetTx`, so their writes ignored any active
    unit of work. On every backend, the engine's variable snapshot was written *outside*
    the instance-update transaction — roll back and the history lies. SQLite made it loud:
    its pool is now one connection (pooled connections deadlock on lock upgrades, immune to
    busy_timeout), which turned the silent escape into a boot hang and led straight to the
    bug. Data migrations had the same flaw — `Transactional: true` over an outer-pool
    repository — and now build their repository over the runner's transaction.
  - **MySQL tests get per-test databases**, mirroring Postgres's per-schema isolation.
    `go test` runs packages in parallel; two packages dropping the same shared tables
    produced failures that read exactly like cross-tenant bugs.
  - **Designer node panels show real API usage** — the previous `ApiExample` pointed at
    routes that do not exist and a client that did not. Every node type now renders the
    genuine curl + SDK for driving that step, drawn from the node's own configuration.
  - Verification: full suite green with live Postgres 17 + MySQL 8; SDK vet/race green;
    UI typecheck/lint/test/build green; quickstart run end to end against a live server.

#### 11. What’s Next (Execution Order)

1. **P0 Security remainder** — the repository tenant scope still fails open when no
   `TenantContext` is present, which is what lets the engine and its workers run. That is
   defence in depth rather than a live hole (both protected chains resolve tenant,
   unresolvable principals are refused, and the public chain's membership is asserted by
   test). The integration coverage that had to exist before the flag could be flipped now
   does — `tests/strictscope`, entering through the real HTTP chain and the job worker —
   so what is left is the staged rollout: staging with the flag on, watching for queries
   that suddenly return nothing, then production, then the default.
2. ~~**P0 Reliability remainder**~~ — the connector contract tier landed
   (`tests/connector/contract_test.go`), which was the last missing tier. Outage
   simulation and feature flags had already landed.
3. **P1** — `golangci-lint` backlog burn-down; order is in `.golangci.yml`.
4. **P2 UX Delight** — Task Inbox SLA fields: overdue countdown and priority badge
   backend fields. Business Timeline is already complete.

**Closed since this list was written:** Phase 2 landed the real FEEL parser, and the
memory-exhaustion vector it existed to remove is now off by default —
`javascript-conditions` ships `false`, so a default installation refuses `js:` conditions
outright rather than handing authored content to a runtime no sandbox setting can bound.
A gateway whose conditions are all refused raises an incident naming the gateway; it does
not guess a branch (`tests/bpmn/refused_js_condition_test.go`). Installations still
migrating can set `GOBPM_FEATURE_JAVASCRIPT_CONDITIONS=true`, and
`GET /api/v1/definitions/javascript-conditions` gives them the worklist.

---

### Verification coverage (2026-08-19)

Two gaps closed, both of the same shape — a test existed and had never run:

- **Every dialect suite skipped everywhere.** `tests/postgres`, `tests/mysqldb`
  and `tests/tenant` skip when their DSN is unset, and no DSN was set in CI,
  which had no database service containers at all. The `dialects` job now runs
  all four against real PostgreSQL, MySQL and SQL Server, and fails if any test
  *skips* — a skip is the failure the job exists to prevent.
- **SQL Server had never created a table.** It is offered in the config and the
  setup wizard; `models.UUID` answered `uuid`, which it has no word for. Fixed to
  `char(36)`, the column MySQL already runs on.

**Known blocker:** GitHub Actions is not running — every job on PR #9 reports
*"The job was not started because recent account payments have failed or your
spending limit needs to be increased."* Until that is resolved, the `dialects`
job cannot prove the SQL Server fix. The column-type decision is unit-tested
locally per dialect (`server/repositories/models/uuid_dialect_test.go`), and the
schema test needs an amd64 machine — the official SQL Server image has no arm64
build and segfaults under emulation.

**Still open, and why:**

- **`golangci-lint` runs clean** as of this date, measured with the caps off:
  `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` → 0 issues.
  The defaults collapse identical messages to three per linter and reported "87
  findings" for 344; measure with the caps off or the number is fiction.
- **PWA, the React compiler and i18n** (§7 lower-priority) need a browser to
  verify and are not attempted blind.
- **Architecture audits** (§2) are documentation of the code as it stands, not
  changes to it.
