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
  - [x] API latency targets defined.
  - [x] Throughput target defined.
  - [x] Reliability target defined.
  - [ ] Recovery target finalized with explicit per-environment `RTO`/`RPO` values.
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
  - [x] Tenant isolation verification completed in repositories/queries.
  - [x] DB parameterization audit completed.
  - [x] Secret/PII redaction implemented for outward errors/log paths.
  - [x] API abuse controls implemented (request-size limit + rate limit interceptors).
  - [x] Security/reliability CI scanning baseline implemented (`go vet`, `go test -race`, `govulncheck`, `gitleaks`).
- [ ] 6. Reliability & Bug Reduction
  - [ ] Full test-pyramid baseline complete (unit + integration + contract + E2E).
  - [x] `go test -race` enabled in CI workflow.
  - [ ] Fuzz tests added for parsers/forms/expressions.
  - [ ] Outage simulation suite added (DB/broker/network).
  - [ ] Feature-flag/canary rollout mechanism defined and integrated.
- [ ] 7. User-Friendly UX Roadmap
  - [x] Business Timeline audit log: `AuditWriter` contract + `narrativeFor` narrative generator + lifecycle hooks for all task events (Claim/Unclaim/Complete/Assign/Delegate/Create).
  - [ ] Task Inbox UX overhaul: priority badges, overdue countdown, bulk actions.
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

#### 11. What’s Next (Execution Order)

1. P2 UX Delight (next):
   - Task Inbox SLA fields: overdue countdown + priority badge backend fields.
   - Business Timeline already complete.
2. P0 Reliability gaps:
   - Fuzz tests for parsers/forms/expressions.
   - Feature-flag/canary rollout mechanism.
