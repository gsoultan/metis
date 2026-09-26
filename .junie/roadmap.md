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
6. Notification System: **done.** The centre and the unread count were already
   built; what was missing was anything feeding them and any way out of the app.
   - In-app center with unread count — already there, and now actually fed.
   - Assignment/incident alerts — the observer read the recipient from the
     instance's business variables, so it told you when you claimed a task
     yourself and not when one arrived for you. It reads the node's assignee and
     candidates now, and says so when work is withdrawn as well as when it
     arrives.
   - Email/webhook notifications — opt-in per installation through
     NOTIFICATION_WEBHOOK_URL and NOTIFICATION_SMTP_*, in the same shape as
     WEBHOOK_ENDPOINTS. Stored first and delivered second: the centre is the
     record, so a mail server that is down does not also cost somebody the one
     place it was guaranteed to appear. The webhook goes through the shared
     client, so it is subject to the same egress policy as every other outbound
     call rather than being a way around it.
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
- [x] 2. Core Backend Architecture — audited 2026-09-25 in
      [`docs/architecture-audit.md`](../docs/architecture-audit.md). Every finding has its
      place in the code, a class, a fix and a size; the defects are fixed (#93–#95, and the
      thousand-row reads in #103) and the eight cheap fixes applied.
  - [x] `ServiceFacade` orchestration-only compliance verified across domains. The struct
        is orchestration-only; six endpoint functions held rules that belong in a service
        (audit §1).
  - [x] Small, consumer-centric interface compliance audit completed (audit §2; fields and
        parameters narrowed to the part they call).
  - [x] Pattern usage audit (`Repository`, `UnitOfWork`, `Strategy`, `Adapter`, `Decorator`, `Observer`) completed (audit §3).
- [x] 3. Performance & Memory Strategy — every item below is done; the parent was
      left unticked, and two of the completed items (`P1-OPT-06`, `P1-OPT-07`) were
      missing from the list though the session log records them. Reconciled
      2026-09-25. How to measure and profile now is `docs/performance.md`: the
      PowerShell harness the 2026-03-24 entries cite (`tests/performance/`) no
      longer exists, and `tests/loadtest` with pprof replaces it.
  - [x] `pprof` CPU/heap baseline under load established.
  - [x] `P1-OPT-01` setup-status request-path overhead trim completed (middleware wrap reuse).
  - [x] `P1-OPT-02` connection-churn guardrails completed (explicit HTTP server keep-alive settings + sustained-load verification).
  - [x] `P1-OPT-03` regex compile hotspot audit and caching/precompile optimization completed.
  - [x] `P1-OPT-04` setup-status rate-limit transient-overhead reduction completed (in-place window updates + client key fast-path parsing).
  - [x] `P1-OPT-05` setup-status auth public-path lookup optimization completed (linear scan replaced with map lookup + auth header parse allocation trim).
  - [x] `P1-OPT-06` optional-auth transient-allocation reduction completed (`strings.Cut` in place of a per-request `strings.Split`).
  - [x] `P1-OPT-07` slice preallocation for the high-frequency gRPC list-response mappers completed.
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
        **Corrected 2026-09-25:** nothing starts the RabbitMQ bridge or the inbound
        consumer (INT-15), so in a running server there is no broker connection for
        that reconnect logic to recover. The unit tests cover code that does not run.
  - [~] Feature-flag mechanism defined and integrated — `internal/pkg/features`,
        used by the strict tenant scope and the system-identity work. **Canary
        rollout is not built**: there is no traffic-splitting or staged-cohort
        mechanism, so a flag is on or off for the whole installation. The shipped
        defaults are pinned by test (`TestSecurityDefaults`), because changing
        either one is a security decision with a rollout plan behind it rather
        than a tweak.
- [x] 9. BPMN interoperability and process mining (2026-09-12)
  - [x] **Diagram interchange round-trips.** Import and export carry shape bounds,
        `isExpanded`, and connector waypoints. Export previously wrote a bare
        `<definitions>` with no namespace and no diagram: valid XML that this
        parser read back, so the round trip looked healthy, and that no other BPMN
        tool would open. The geometry has a field at every layer it crosses —
        entity, database model, protobuf, designer save request — because the
        adapters copy field by field and a missing one is dropped in silence.
        Covered by `server/domains/services/impl/bpmn_xml_diagram_test.go`,
        `server/domains/adapters/definition_geometry_test.go` and the geometry
        cases in `ui/src/mappers/definitionMapper.test.ts`.
  - [x] **Export stopped dropping nodes.** `classifyNodes` had no case for a
        sub-process, so exporting one produced a valid file with the sub-process
        and its children missing, and reported success. Pools, lanes, escalation
        and compensation throws and the terminate marker went the same way.
  - [x] **Execution-affecting attributes survive.** Gateway `default` flow,
        `calledElement`, `cancelActivity`, multi-instance loop characteristics,
        and Camunda-namespaced topic/assignee/formKey. The default flow matters
        most: the engine refuses to guess at a decision point, so losing it turned
        a working diagram into one that raises an incident.
  - [x] **Conditional events.** New in both the engine and the designer. Evaluated
        on arrival and again at the end of every advance of the same instance,
        which is the only thing that can make the condition true. Re-evaluation
        reads the tokens rather than a subscription table, so there is no new
        state and no migration. `tests/bpmn/conditional_event_test.go`.
  - [x] **A catch event with nothing to wait for is refused.** It used to return
        success and leave the token in place — a permanent hang with no incident
        and no log line.
  - [x] **Ad-hoc sub-processes are authorable.** The engine has run them for a
        while; nothing in the designer could produce one. `SubProcessConfig` plus
        `ui/src/domain/adHocSubProcess.ts`, which refuses an ad-hoc group with no
        steps in it and one that is also event-triggered.
  - [x] **OCEL 2.0 export** at `GET /api/v1/projects/{id}/ocel`. The activity is
        the node name, not the audit kind — the obvious mapping discovers a model
        with four boxes in it. Instances relate to definition *and version*.
        Process variables are excluded unless `?include_variables=true`: the audit
        data map is the instance's business facts and a control-flow model needs
        none of them. Tenant scope comes from the repository, as it does for every
        other project-scoped read.

- [x] 10. Connector delivery is proven, not assumed (2026-09-12)
  - [x] **RabbitMQ publishes are confirmed and mandatory.** Pointing the connector
        at a real broker for the first time found that the advertised
        "Queue (Direct Publish)" configuration published to the default exchange
        with an empty routing key and delivered nothing, while returning
        `{"status": "published"}`. The unreachable fallback beside it only ran on
        a publish error, which fire-and-forget publishes never produce.
        `tests/connector/broker_test.go` failed on all three cases before the fix.
  - [x] **The broker suite runs in CI.** `METIS_TEST_RABBITMQ_URL` plus a
        `rabbitmq:3-alpine` service on the `go-security-reliability` job. The
        existing "No suite skipped for want of a database" step fails the build on
        any skip, so the gate cannot silently stop running.
  - [x] **SMTP is tested against a server that speaks SMTP.**
        `tests/connector/smtp_test.go` runs an in-process server and asserts the
        envelope sender, recipient, subject header and body. Hermetic, so it needs
        no gating and always runs.
  - [x] **Both publish paths in `messaging.go` confirm now.** The logic the
        connector already had — publisher confirms plus mandatory delivery, so a
        message the broker refused or could not route is an error rather than
        silence — was written out inside the connector and used by nothing else.
        It now lives in `amqp_confirm.go`, and all three publish paths share it;
        the connector's broker-backed tests cover the shared code.
        - The external-task bridge no longer logs "Forwarded external task to
          RabbitMQ" over a task that never left. A publish the broker did not
          accept hands the task back through `HandleFailure` instead of letting
          it sit locked for the full 30s, so the engine's own retry decides what
          happens next.
        - The inbound consumer acknowledged every message the moment the broker
          handed it over, so every path that tried to preserve a message it
          could not process was preserving one the broker had already forgotten.
          It takes manual acknowledgement with a prefetch of one and settles
          each message on the outcome: parked in the DLQ is an ack, a DLQ
          publish that failed is a requeue, and a dispatch abandoned because the
          engine is shutting down is a requeue rather than the silent drop it
          used to be.
        - Covered without a broker, so it always runs:
          `TestAMessageTheDeadLetterQueueRefusesIsNotAcknowledged`,
          `TestAMessageParkedInTheDeadLetterQueueIsAcknowledged`,
          `TestAnUnreadableMessageTheDeadLetterQueueRefusesIsNotAcknowledged`,
          and the shutdown case folded into the existing dispatch-timeout test.
          All four verified to fail against the previous behaviour.

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
  - [ ] Medium-priority UX items (5-8) delivered. Delivered: 5, the heat map, the deadline
        report and CSV export, all counted on the server (#97), and the PDF export (a printable report, 2026-09-26); 6, done
        before; 8, version comparison, rollback and migration (#98). Item 7 in part:
        memberships stay inside an organization, role refusals are 403, and anybody can
        edit their own profile (#96). The visual role editor is not built.
  - [ ] Lower-priority UX items (9-12) delivered. Delivered: 10, the decision-table editor
        (#101); 11, progressive disclosure (#100); 12, onboarding and help (#102).
        Item 9 in part: manifests are hardened (#99). Listing installed manifests in the
        designer's catalogue waits on a security decision, because manifests are
        installation-wide and roles global (`roadmap-connector-catalogue`).
- [ ] 8. 90-Day Execution Plan
  - [x] Phase 1 complete (`baseline profiling + SLO dashboard`, `top 5 bottlenecks`, `critical security gaps`).
        Profiling: the pprof baseline and `P1-OPT-01`–`08`, and contention profiles
        that record something (2026-09-25). SLO dashboard: `deploy/grafana/metis-slo.json`
        over recorded burn-rate ratios. Bottlenecks, each measured before it was
        fixed: the job claim query (18.8ms → 0.04ms at 100,000 due), the retention
        sweeps (by ctid), the storm pool's constructor and size, and the `P1-OPT`
        request-path items. Security: the eight P0s of 2026-09-25.
  - [x] Phase 2 complete (`architecture cleanup`, `high-value UX improvements`).
        Cleanup: the audit and its eight cheap fixes; transactions and idempotency in
        #93–#95. UX: #89 and #96–#102.
  - [ ] Phase 3 complete (`load/chaos`, `canary + hardening`, `playbooks/docs`). Load/chaos:
        #103. Hardening: key rotation (#91) and the fixes since. Playbooks: the
        runbooks (`docs/runbooks.md`), held to the alerts by a drift test. The canary
        rollout waits on a decision: flags are installation-wide, and an organization
        cohort needs a tenant where the flag is read.

#### 10. Session Execution Log

- This session should prioritize concrete, verifiable roadmap steps over broad rewrites.
- Any completed recommendation must include code/config changes or explicit verification evidence.
- 2026-03-24 (completed): Executed P0 Security/ Reliability CI baseline.
  - Added `.github/workflows/security_reliability_ci.yml`.
  - Coverage now includes:
    - SAST baseline: `go vet` on `./cmd/metis ./internal/app ./server/interceptors/...`.
    - Reliability baseline: `go test -race` on `./internal/app ./server/interceptors/...`.
    - Build gate: `go build ./cmd/metis`.
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
    - `go build ./cmd/metis`
- 2026-03-24 (completed): Executed P1 profiling baseline (`pprof`) with guarded runtime exposure and measured hotspots.
  - Added guarded profiling server in `internal/app/app.go`:
    - Enabled only when `METIS_PPROF_ENABLED=true`.
    - Configurable address via `METIS_PPROF_ADDRESS` (default `127.0.0.1:6060`).
    - Lifecycle-managed startup/shutdown using existing app errgroup and timeout pattern.
  - Added focused tests in `internal/app/profiling_test.go`:
    - Env parsing and default address resolution.
    - `pprof` handler route availability.
  - Runtime baseline workload used for measurement:
    - Repeated requests to `GET /api/v1/setup/status` while collecting `pprof` CPU (`20s`) and heap snapshots.
  - Top 5 baseline hotspots and targets:
    - CPU (flat): `runtime.cgocall` (`57.75%`) → target `< 40%` by reducing per-request socket churn and improving keep-alive reuse.
    - CPU (cum): `net/http.(*conn).serve` (`59.69%`) → target `< 45%` by trimming request-path overhead on high-frequency endpoints.
    - CPU (cum): `github.com/gsoultan/metis/internal/app.(*App).runServers.func2` (`23.64%`) → target `< 15%` via handler/interceptor allocation reduction.
    - Heap (flat): `runtime.mallocgc` (`30.43%`) → target `< 22%` by removing avoidable transient allocations.
    - Heap (flat): `regexp.compile` (`12.15%`) → target `< 3%` by precompiling/caching regex construction.
  - Verification evidence:
    - `go test ./internal/app`
    - `go build ./cmd/metis`
    - `go tool pprof -top http://127.0.0.1:6060/debug/pprof/profile?seconds=20`
    - `go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap`
- 2026-03-24 (completed): Executed P1 reproducible load/profiling harness and concrete optimization backlog definition.
  - Added reproducible script: `tests/performance/setup_status_profile.ps1`.
  - Script behavior:
    - Resolves `ENCRYPTION_KEY` from `config.yaml` and applies it for deterministic startup (falls back to pre-set env var only if config key is unavailable).
    - Starts `metis` with guarded `pprof` env flags.
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`
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
    - `go build ./cmd/metis`

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
    - `go build ./cmd/metis`

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
    - `go build ./cmd/metis`
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

- 2026-09-25 (completed): 90-day plan Phase 1, "fix critical security gaps" — eight P0s
  found by auditing the code against §2, §7 and §8 before starting any of them. Each is its
  own commit with a test that fails against the code before it; branch `roadmap-open-items`.
  - **Setup never closed on a container deployment** (IAM-08). "Set up" meant config.yaml
    existed, which a server started from `DATABASE_URL` never writes — and on a read-only
    root cannot. The wizard stayed open to anybody: with the credentials the evaluation
    stack publishes it minted an administrator inside the live database. It now closes once
    the running database has any account, seeds only an empty database under an advisory
    lock, and — when the environment names the database and both secrets — asks only for
    the organization and first administrator and writes nothing. That also made the
    container first run work at all: it used to fail writing config.yaml every time.
    Driven end to end against the built binary from a read-only directory.
  - **gRPC listened on :8081 by default with no authentication**, published by the image,
    compose and the manifest; `CreateOrganization` sat on the public chain beside it
    (SEC-01/IAM-16). gRPC is opt-in (`METIS_GRPC_ADDRESS`), the endpoint is admin-only, and
    the wiring test asserts the public set exhaustively.
  - **An administrator of one organization could read, change or delete another's
    accounts** — its last administrator included. Scoped to the caller's organizations, and
    nobody may remove an organization's last administrator.
  - **A business rule task could run another tenant's decision table** (DMN-12): the key
    lookup was unscoped under the system identity the job worker uses. Decisions are
    resolved in the process's project.
  - **A job left running by a dead worker was never picked up again**, and neither was an
    external task whose lease expired — the latter because the generated query builder drops
    the predicate after a null test inside `Any` (a storm defect; worked around by order).
    Reclaiming is counted as an attempt, a job's status commits with its work, and a service
    task checks its token before advancing — without that, reclaiming advanced processes
    twice. Claim query measured before choosing its shape (18.8ms vs 0.04ms at 100,000 due).
  - **Notification delivery made network calls inside the engine's transactions** (arch
    veto), SMTP with no timeout at all. Delivered after commit on a bounded queue; both SMTP
    paths have a deadline.
  - **Each open browser tab held one of the API's 128 backpressure slots**, and streams were
    recorded as hour-long GETs in the SLO histogram. Streams have their own caps, a
    heartbeat, and their own gauge.
  - **Eight tables held plaintext copies of encrypted variables** (SEC-05). Sealed; the
    README now says exactly what is encrypted, including that older audit rows are not.
  - **Found and not fixed here** — for the backlog, in roadmap order:
    - Three merged PRs never reached `main`: #75 (rollback wording), #78 (withdrawal
      notifications) and #82 (the step heatmap) were merged into base branches that had
      already been squash-merged. The repository deletes no head branch on merge, so
      GitHub did not retarget the stacked PRs. §7 item 6 above describes #78 as shipped.
      **Re-landed in the engine-reliability batch below.**
    - The storm query builder mishandles `Any(IsNull, …)`; report it upstream. **Not a
      storm defect:** the store had been generated by v0.10.0 and never regenerated after
      the bump to v0.15.0. Regenerated in the engine-reliability batch below.
    - Historic rows written before sealing stay in plaintext until rewritten; a batched
      backfill would close it.
    - P0.2(c) (a memory bound for scripts) is still open — see `security-plan.md`.

- 2026-09-25 (completed): 90-day plan Phase 1, engine reliability — branch
  `roadmap-engine-reliability`, stacked on `roadmap-open-items`. Each fix is its own commit
  with a test that fails against the code before it.
  - **Three merged PRs re-landed.** #75, #78 and #82 had been merged into base branches
    that were already squash-merged, so none of them reached `main`. Cherry-picked here.
  - **The store was generated by a different storm than the one the module runs.** #66
    moved go.mod from v0.10.0 to v0.15.0 without regenerating. The v0.10.0 builders dropped
    the predicate after a null test inside `Any`, which is the "storm defect" the batch
    above worked around. Regenerated. `tests/drift` now fails when a generated header names
    another version than go.mod pins, and runs that query against a database.
  - **A loop back through a service task replayed its first answer** (EXE-11). A call was
    recorded once per step, so the second visit found the first visit's response and moved
    on without calling. Calls are recorded per visit. Migration 23 swaps the unique index
    concurrently, and a call in flight during the upgrade is adopted by the visit that made
    it, keeping the key the partner has already seen.
  - **A message or signal started one instance per stored version** of every matching
    process, each at the definition's first start event. Only the live version listens now,
    and it starts at the start event the message names.
  - **A timer moved by a migration never fired** (OPS-08). The job kept the old version, so
    it looked for the new node in the old definition and dismissed itself.
  - **Two steps of an ad-hoc sub-process started at once lost one.** The token list was
    read, changed and written back whole, with no lock. Activation now runs in one
    transaction on the locked instance.
  - **Three tables were never swept:** webhook deliveries, idempotency records and the
    shared rate-limit counts, two of them keyed by caller input. Swept at start-up and
    every ten minutes, 5,000 rows per statement, picked by ctid. Picked by key, every batch
    planned as a scan of the whole table.
  - **Every task action was audited twice:** once by the task service and once by the
    audit observer from the event. The service's entry, which names who acted, is kept.
  - **Not done, and why:** INT-15 (the RabbitMQ bridge and consumer are never started) is
    an open question in the PRD: wire them up with configuration, or remove them and their
    docs. It waits for that decision.
  - **Found and not fixed — for the backlog:**
    - A webhook signature covers the body only. The delivery-ID header is unsigned and
      nothing is timestamped, so a captured delivery can be replayed under a new ID.
      Closing it needs senders to sign a timestamp as well.
    - An idempotency claim left by a replica that died blocks its key until the sweep
      removes it after a day. Meanwhile every retry with that key waits out the 10s budget
      and fails.

- 2026-09-25 (completed): 90-day plan Phase 2, "UX improvements" — the core paths that
  were broken outright, before any new UX. Branch `roadmap-core-ux`, stacked on
  `roadmap-engine-reliability`. Each fix has a test, or a type or lint guard, that the code
  before it fails.
  - **A form built in the designer never reached its task** (HUM-11). The designer saves
    a form as a list of fields, and the task copied it with a string-only read that
    answers "" for a list. Task creation and migration now keep text as it is and encode
    anything else as JSON.
  - **The task inbox never updated live** (HUM-16). It opened an `EventSource`, which
    cannot send the Authorization header, so the events endpoint answered 401. It uses
    the authenticated stream now, also listens for withdrawn tasks, and ESLint refuses
    `new EventSource`.
  - **"Deploy Anyway" staged instead of deploying** (MOD-04). The button passed its click
    event as the `stage` flag, and it also skipped the question a clean deploy asks when a
    live version would be replaced. The deploy mode is a required `'live' | 'staged'`, so
    passing a click handler is a type error, and both buttons share `nextDeployStep`.
  - **Importing a BPMN file always failed** (MOD-08): no project was sent. Non-Latin-1 names
    also broke both directions: `btoa` threw on import, and `atob` garbled the export.
  - **Saving a decision table erased its examples** (DMN-10), and the examples panel was
    never mounted. Loading and saving are now a pair, so an untouched table saves back what
    was stored.
  - **The versions page named the oldest promoted version as live** (DEF-09), and did not
    tell a scheduled cutover from one that had happened.
  - **Execution paths came back end to start** (OPS-02). The code assumed the audit trail
    comes newest first, and it comes oldest first.
  - **Not done, and why:** DMN-06 (saving a decision rewrites that version in place) is an
    open question in the PRD: whether saving should always create a new version, with
    drafts kept separately. It waits for that decision.

- 2026-09-25 (completed): 90-day plan Phase 1, "baseline profiling + SLO dashboard" —
  what the engine does when nobody is watching it. Branch `roadmap-observability`,
  stacked on `roadmap-core-ux`.
  - **The strict scope's soak alert could never fire.** `count` drops every label and a
    plain `and` matches on labels, so the documented expression was always empty. It is
    in `alerts.yaml` now, unit-tested and matched per instance.
  - **The engine's backlog and pools are measured**: due jobs and the oldest one's age,
    expired leases, open incidents, and both connection pools. An unreadable backlog
    reports `up 0` rather than an empty queue. Each has an alert and a runbook entry.
  - **The error budget is watched by burn rate** (14.4×, 6×, and a 3-day average) over
    recorded ratios, with `deploy/grafana/metis-slo.json` over the same numbers. The
    old alert paged at 1× — a system spending its budget exactly as planned.
  - **The storm pool ignored the documented pool size and bypassed storm's
    constructor.** Its size came from the machine's CPU count, and a text query mode —
    common behind PgBouncer — would have decoded booleans inverted instead of being
    refused. Environments' pools lacked the health checks too. One constructor now
    serves both.
  - **The mutex and block profiles were always empty**: no sampling rate was set.
  - `tests/drift` now fails when an alert has no runbook, when an annotation names a
    runbook section that does not exist, or when a rule or dashboard panel reads a
    metric the code does not export — which promtool's tests cannot catch.
  - Runbooks for out of memory and secret rotation, `docs/performance.md`, the
    security plan's status, §9.3 reconciled, the broker-reconnect claim corrected,
    and the tests moved off the pre-rename variable names before that fallback expires.
  - **Found and not fixed — for the backlog:**
    - **`ENCRYPTION_KEY` cannot be rotated.** Sealed values are encrypted under the
      one key and nothing re-encrypts them, so a leaked key cannot be retired. The
      ciphertext is already prefix-tagged, which is the start of a keyring: new key
      for writes, old keys for reads, and a batched re-encryption. **Fixed in the
      key-rotation batch below.**
    - The engine gauges read the main database only; an environment's jobs are not
      counted.
    - No broker/DLQ runbook: the consumer it would cover is never started (INT-15).

- 2026-09-25 (completed): 90-day plan Phase 3, "hardening" — `ENCRYPTION_KEY` can be
  rotated. Branch `roadmap-key-rotation`, stacked on `roadmap-observability`.
  - **The gap.** One key sealed and opened everything, so changing it made every sealed
    value unreadable, and a key that had leaked could never be retired. The docs said
    so, and the only advice was not to rotate.
  - **The keyring.** `ENCRYPTION_KEY` seals and is tried first. `ENCRYPTION_KEY_PREVIOUS`
    is only ever read with: GCM authenticates the ciphertext, so a wrong key fails
    cleanly and the next one is tried. config.yaml's connection string reads under
    it too, since without that the server cannot reach its database after a rotation.
  - **`metis --reseal`** finds sealed values by their `gcm1:` prefix in every text,
    json, jsonb and bytea column of every table, rather than trusting a list of columns
    — a column a list missed would be data lost the moment the old key goes. It covers
    the main database, each environment's, and config.yaml. It works in batches, in
    ctid order, and each update only lands if the value is unchanged since it was read.
    `--reseal-check` fails while anything is left under the old key.
  - The integration test found a fourth storage form the scan first missed: a sealed
    map JSON-encoded into a *text* column (`jobs.payload`).
  - **Driven end to end** with the built binary. Under the new key alone, an instance
    sealed under the old one failed with `cipher: message authentication failed`. The
    check then exited 1 ("7 value(s) are still sealed under a previous key"). The
    reseal moved 7 (`audit_logs.data` 4, `process_instances.variables`,
    `tasks.variables` and `variable_snapshots.variables` 1 each), the check exited 0,
    and after a restart under the new key alone the instance read back its variables.

- 2026-09-25 (completed): 90-day plan Phase 3, "load/chaos testing". Branch `roadmap-chaos`.
  - **Concurrent writes** (`tests/loadtest`, opt-in): 16 writers complete the two branches
    of a parallel approval in a seeded random order. For one instance in four both are
    sent at the same instant, and one submission in eight is sent twice. The job worker
    runs the service task after the join. Every task is completed once, no instance is
    left at the join, the partner gets one call per instance under its own key, and
    nothing fails with a deadlock or a serialization error. Completion p95 is 19-22ms at
    200 instances and 31ms at 5,000; throughput 314-424 instances/s
    (`docs/performance.md`).
  - **A worker's connection killed mid-job** (`tests/outage`). The worker holds no
    transaction across the partner's call, so the faults are aimed at the two writes
    after it. The job is retried, the partner is called once (or twice under one key),
    and the instance finishes.
  - **Found and fixed**, each with a test that failed first. A second submission of a
    completion was answered 500; it is a 400 refusal now. storm's default limit of 1,000
    rows silently truncated reads the engine acts on: a signal's audience, a migration's
    instances and jobs, the decision delete guard, the withdrawal of a deadline's open
    tasks, and the shared rate limit's totals.
  - **Open**: capped reads behind views and exports (an instance's audit trail and
    execution path, the OCEL export, the dashboard's step heat map, users and group
    members, sub-processes, incidents). Also the definitions list that the decision guard
    and message and signal start events walk (over 1,000 versions in one project), the
    last-administrator guard (over 1,000 members), and the one-time backfills v2 and v3.
    Separately, paged lists that order by creation time alone repeat and drop rows across
    a page boundary when one transaction created them.

- 2026-09-26 (completed): the rest of the roadmap's open items, as a stack of PRs merged
  in order (#92 up to the architecture audit's PR), each fix with a test that fails without it:
  - #92: a migration request that omits `dry_run` is a dry run, as documented.
  - #93, engine correctness: hand-over checks and task-row locks (release, reopen, claim
    and release/edit races); webhook receipt in one unit of work and the event webhook
    after commit; external-task failures raise incidents, the retry wait is honoured, and
    a sweep re-offers tasks stranded at zero retries; unknown step and repeat types are
    refused at deploy; a failed migration skip leaves the task; a service task does what
    the modeller chose.
  - #94: a deleted or disabled environment stops being served, without a restart.
  - #95: a live-update hint is sent once the work it points at has committed.
  - #96: access control (organization-scoped group membership, 403 for a missing role,
    self-service profile, case-insensitive roles).
  - #97–#102: UX items 5, 8, 9 (in part), 11, 10 and 12, as §9.7 records.
  - #103: load/chaos testing and the thousand-row reads (entry above).
  - The last PR: the architecture audit and its eight cheap fixes.
  - Decisions left open are in the final report and in the Serena memory
    `verified-findings`: manifest scoping and the built-in override, the canary cohort,
    OIDC users' organizations, the RabbitMQ bridge, decision versioning, whether a
    service task's script runs, and task-edit authorization.
- 2026-09-25 (completed): The strict tenant scope's rollout became observable (§11 item 1).
  The scope's failure mode is silence, and the rollout doc's own advice was to watch for a
  log line that appears once per call site. `internal/pkg/metrics.NewTenantScopeCollector`
  reports, at scrape time, whether the flag is on and one series per denied site, labelled
  with the site — both, because zero denied sites reads as "clean" only while the flag is
  actually on. Bounded by code, not traffic: sites are keyed by program counter. The
  metrics endpoint is its own opt-in listener, not the API port, so naming code paths there
  is an operator's view. Not done here, and not doable from code: the soak against a real
  workload, production, and retiring the flag.

- 2026-09-25 (completed): Executed `P0-SEC-07` — a credential inside a value is masked.
  A RabbitMQ connection's `url` carries the broker password (`amqp://user:password@host`),
  and masking went by key name, so `ListConnectorInstances` — any signed-in account —
  returned it in clear. `configsecret.CarriesCredential` now recognises a URL with a password
  in its user information, or a query parameter named like a secret, whatever its key.
  The whole value is masked rather than the password cut out, so there is no rebuilt-URL
  format to get wrong. Test: `tests/connector/url_credential_test.go` — through the endpoint,
  the broker url came back in clear before and is masked after; saving the form unchanged
  keeps the stored url.

- 2026-09-25 (completed): Executed `P0-SEC-06` — only an administrator can make the server
  connect somewhere. Found while building `P2-INT-02`.
  - **The hole.** `POST /api/v1/connectors/execute` runs a connector with a configuration its
    caller writes, and was wired to `protected` — signed in, nothing more. The SMTP and AMQP
    connectors dial directly (no egress guard), so any account could point one at any host
    and port and read from the error whether something listened: a port scanner, run from
    inside the network Metis sits in. Proven through the real HTTP chain: a `USER` account
    made the server open a connection to a listener on 127.0.0.1 and got `send mail: EOF`
    back.
  - **Fix.** `adminOnly`. Not an egress guard on SMTP and AMQP: that guard refuses private
    networks by default, and an organisation's mail relay is normally on one. The connection
    test on the Connectors page is the only working caller and is an administrator's page.
  - **Also.** The RabbitMQ connector's per-URL connection map was unbounded and kept stale
    connections for good; it is a bounded LRU now, closing what it pushes out, with dials
    shared so two first publishes cannot leak a connection between them.
  - Test: `tests/connector/execute_authorization_test.go` — a listener stands in for an
    internal host; `USER`, `OPERATOR` and `DESIGNER` are refused and it sees no connection,
    `ADMIN` still reaches it. Fails before (three connections), passes after.

- 2026-09-25 (completed): Executed `P2-INT-02` — a process can look something up in its own
  database before it decides. Branch `database-lookup-connector`.
  - **Reprioritization note.** This is P2 work landing while §11 item 1 — the staged rollout
    of the strict tenant scope — is still open. Asked for directly by the product owner, so
    taken out of order deliberately. The open P0 is an operational rollout (staging soak,
    then production, then the default) rather than code, and nothing here changes it. The P0
    defects this work uncovered were fixed first, in their own commits, ahead of the feature.
  - **Problem.** A decision is only as good as the data the process carries, and the data that
    matters usually lives in the customer's own database. Reaching it meant writing and hosting
    an API in front of that database for an HTTP task to call.
  - **Acceptance criteria**, each executable by a non-author and each covered by a test:
    1. An administrator connects a project to PostgreSQL, MySQL or SQL Server once, on the
       Connectors page; the connection string is encrypted at rest and never returned to a
       browser.
    2. A designer drags *Database Lookup* onto the canvas, writes a `SELECT` with `:named`
       values, maps each to a process variable or FEEL expression, and names one variable for
       the answer.
    3. The answer is exactly one variable — `x.row`, `x.rows`, `x.row_count`, `x.truncated` —
       and a gateway or decision table after the step routes on it
       (`tests/sqlconnector/lookup_decision_test.go`: gold and silver customers take different
       paths; an unknown one takes the default with no incident).
    4. Deploying a process with a lookup needs the `QUERY_AUTHOR` role; a designer without it
       is refused with the reason, at every deploy path including a lookup nested in a
       sub-process.
    5. A query that writes, runs two statements, reads files or reaches another server is
       refused; values are only ever parameters; a lookup is stopped at its time limit and
       bounded in rows and bytes; a login that can see Metis's own tables is refused.
  - **Design.** A connector on the service-task path, not a new BPMN element — so it inherits
    retries, incidents, error boundaries, the breaker and the rate limit, and reaches the
    palette from its catalogue row. A step's own statement reaches the executor through
    `ConnectorRequest` and an optional `RequestExecutor` interface; the ten existing executors
    are called exactly as before (asserted). The request runner is deliberately kept off the
    service facade, which every endpoint can reach.
  - **Deliberate divergence from the plan, stated:** the query is written by the process's
    designer, not by an administrator as a catalogue of named queries. Decided by the user
    with the trade-off stated in writing: anybody who can deploy a lookup can read what the
    connection's login can read. The login is therefore the boundary that matters; the docs
    and the connector's own settings form say so.
  - **Found and fixed on the way, all P0:**
    - `P0-SEC` — a Postgres participant directory's DSN, password included, was returned to
      the browser: `configsecret` had no fragment for a connection string. Also Slack, Discord
      and Teams webhook URLs, which are the credential. The browser's copy of the list had
      drifted four fragments behind; `tests/secretdrift` now holds the two together.
    - `P0-REL` — headers typed into an HTTP connection were never sent: the catalogue saved
      them as text and the shipped executor read only an object. The one test of headers ran a
      second implementation that was registered and then overwritten. Each built-in is now
      registered once, and the tests run the code that ships.
    - `P0-REL` — a connector step dragged from the palette had no circuit breaker and no rate
      limit: its target key was empty. Now keyed on project and connector, never the shared
      catalogue id alone, which would couple one tenant's outage to another's.
    - `P0-REL` — the CI step "No suite skipped for want of a database" could never fail: it ran
      `go test` without `-v`, and `--- SKIP` is only printed with `-v`. Fixed, with
      `tests/loadtest` excluded since its own job guards it. Every skip in the main job had
      been going unreported.
  - **Found and not fixed — for the backlog:**
    - `P0-SEC` — **fixed in `P0-SEC-06`, below.** `POST /api/v1/connectors/execute` needs only a login. Any signed-in account,
      task-inbox participants included, can make the server connect wherever a
      caller-supplied configuration points: SMTP, AMQP, HTTP (the last is egress-guarded,
      the first two are not). The lookup refuses to connect through it, so this change adds
      no exposure; the endpoint itself wants `designer` or `adminOnly`.
    - The designer's "Try it" sends the connector's id where the endpoint expects its key, and
      the step's input mapping where it expects the connection's settings; it cannot work for
      any connector. **Fixed 2026-09-25** with `POST /connectors/try-step`: the step runs
      against its project's saved connection, so a designer chooses what is sent and never
      where it goes. A lookup needs the Query author role to try, as to deploy.
    - A service task's SENDING/RECEIVING mapping tables write `inputs`/`outputs`, which no Go
      code reads. **Fixed 2026-09-25** without the migration risk: the engine honours
      `input_mapping`/`output_mapping` on connector steps (keys nothing had written there),
      and the designer moves an older step's tables to them when it is opened — so a
      deployed definition changes only when somebody deploys it again.
    - A RabbitMQ `url` can carry a password inside it and is not masked. **Fixed in
      `P0-SEC-07`, below** — by recognising a credential in the value rather than by a
      declared `Secret` flag, which would have left every manifest-installed connector to
      remember to declare it.
  - Gate: `make gate` green with both test DSNs set — ui-build, build, vet, test (78 packages
    ok), race (78 ok, no races), strict-scope (78 ok), tsc, eslint (0 errors), bun test (705
    pass). Also golangci-lint 0 issues, govulncheck no reachable vulnerabilities, gitleaks no
    findings. The MySQL and SQL Server suites run in CI against service containers; locally
    they skip for want of a server.
  - Perf proof (`sqlconnector.BenchmarkALookup`, Apple M5 Pro, PostgreSQL 17 on loopback): a
    lookup on a kept pool ~104 µs / 7.4 KB / 116 allocs; opening a pool each time ~2.7 ms /
    99.8 KB / 444 allocs. Loopback has no network or TLS handshake, so the real gap is larger.

- 2026-09-22 (completed): The rest of the in-flight migration gaps — subscriptions,
  concurrency, resumability and segregation of duties. Closes §6 of
  [`../docs/process-change-in-flight.md`](../docs/process-change-in-flight.md).
  - **A completion could be authorised against a stale task.** `CompleteTask` took the
    instance lock *after* deciding who was allowed to complete, so the decision was made
    against a row another transaction was free to rewrite before the write landed. It now
    authorises (so an unauthorised caller is still told "forbidden" and not "no such
    instance"), takes the lock, then re-reads and re-checks; migration takes the same lock
    for the whole rewrite. Argued rather than demonstrated — the window is a few statements
    wide and `race_test.go` does not reliably land inside it, which its comment says.
  - **Completing a task recorded that a step had happened and not by whom.** A task routed
    by candidate group completed with a nil assignee. The completer is now recorded, which
    is what a segregation-of-duties rule needs and what an auditor asks for first.
  - **Segregation of duties** is declarable: a node names the steps whose performer may not
    also perform it, refused at claim *and* at completion, scoped to the instance because
    the control is about one transaction rather than a permanent bar. Removing a step a
    surviving rule names is a warning — the rule survives the edit and is then satisfied by
    everybody, because nobody performed the step it names.
  - **Resumability without a run table.** The source version already is one: an instance
    that moved is no longer on it, so re-running the same migration picks up what is left.
    Made true rather than plausible by skipping anything not still running — which also
    fixes a finished instance being repointed at a graph it never executed — by making
    `hold` idempotent, and by tagging every entry of one run with a shared `run_id`.
  - Deliberately not built: a `migration_runs` table. Two tables, a storm model, a GORM
    model, codegen and a schema migration to record something the existing rows already
    say.
  - Gate: `make gate` green. 6 new Go tests here (plus 5 in the batch below), each verified
    to fail with its fix reverted except the race invariant and the SoD opt-out, both of
    which say so.

- 2026-09-22 (completed): Node actions — a migration can now decide work instead of only
  moving it. Builds on the in-flight migration work below.
  - A node mapping can only answer *where does this work go*. Removing an approval asks
    whether the approval that was pending counts as given or as void, and the only way to
    say the first with a mapping alone was to point the task at some other step — which is
    how somebody else's approval gets performed by the wrong person.
  - **`skip`** cancels the work on a node and advances the instance past it as though it had
    been performed. The engine's own `Proceed` runs, so boundary timers are cancelled,
    multi-instance counts are honoured and the following gateway is evaluated exactly as it
    would have been; a second copy of that here would be a second set of BPMN semantics.
    The advance runs on the *source* graph, because the node being skipped is the one the
    new version does not have.
  - **`cancel`** ends the instance where it stands and does **not** migrate it: it will never
    run again, so its record should name the version it actually ran.
  - **`cancelled` is a new instance status.** Reusing `completed` would make an instance
    somebody called off read, in every list and count, exactly like one that succeeded;
    `failed` is no better, because nothing went wrong. The UI status vocabulary already had
    an entry for it. Pending timers need no cleanup — `timerStillApplies` already refuses to
    fire for an instance that is not active, which a terminate end event relies on too.
  - Refused: a decision with no reason (without it the trail cannot tell a step nobody
    performed from a step somebody did), a node that is both mapped and actioned, skipping a
    gateway (which branch would it take?), skipping a node with no outgoing flow, and a skip
    in a wiring with no engine. Work on an actioned node is exempt from the must-land check,
    without which the feature is unreachable.
  - New trail entries `node_skipped` and `instance_cancelled`, separate from the migration
    entry because they are the separate fact an auditor asks about: not *this instance
    changed version* but *this approval did not happen, and here is who said so and why*.
  - `NewMigrationService` now takes the engine, and is constructed after it in
    `NewServiceFacade`.
  - Gate: `make gate` green. 8 new tests in `tests/instancemigration/actions_test.go`, each
    verified to fail with its fix reverted; 2 new UI domain tests.

- 2026-09-22 (completed): Changing a process that is already running — in-flight migration
  made safe. Analysis and the remaining gaps: [`../docs/process-change-in-flight.md`](../docs/process-change-in-flight.md).
  - **Node-keyed instance state was stranded by every rename.** `apply` wrote tokens, tasks
    and jobs; `CompletedNodes`, `CompensatedNodes`, `MultiInstance` and `Joins` are keyed by
    node id too and were left pointing at the old graph. A renamed join gateway therefore
    waited forever for a branch that had already arrived — no error, no incident, the
    instance simply never finished. A renamed multi-instance node re-asked everyone who had
    already answered. A stranded `CompletedNodes` lets an activity run twice.
  - **A migrated task kept the authority of the step it came from.** A task mapped from
    `opsApprove` to `salesApprove` kept the operations manager as assignee and candidate
    group. The person whose approval step was deleted could complete the approval step that
    replaced it, and the sales manager never saw it. Tasks that change node are now rebuilt
    from the node they land on, and the claim is dropped.
  - **Migrations left no trace at all.** `instance_migrated` now records source and target
    version, what was re-pointed, who authorised it and which controls were waived. The
    OCEL export inherited the gap; a migrated trace is not a deviation, but only this event
    can say so.
  - **A version a scheduled cutover needed could be deleted.** It has run nothing, so every
    check passed it — and at the scheduled moment the timeline named a version that was not
    there, so the highest one went live instead. Unattended, silently.
  - New refusals: bookkeeping with nowhere to land, two counters merging onto one node, a
    boundary event moved off the activity it guards. New warnings, kept apart from refusals:
    a downstream gateway with a default flow (which routes silently rather than raising an
    incident when a removed step's variables are missing), and tasks held by somebody right
    now. Plans also list the nodes the new version removed.
  - **Compliance holds**: a node marked `compliance_relevant` produces a refusal the operator
    accepts by name, recorded with the authoriser on every instance's timeline. Only
    instances that have not passed the step count, and the obligation must land on a node
    that carries one too — mapping a control step onto an unmarked one lands the token and
    still removes the control. Entirely additive.
  - Sub-process children are now indexed, so a token parked inside one no longer looks
    unlandable.
  - Gate: `make gate` green — `go build`, `go vet`, `go test ./...`, `go test -race ./...`,
    strict-scope, `tsc -b`, `eslint`, `bun test` (731 UI tests). 14 tests in
    `tests/instancemigration/state_test.go`, plus two in `tests/bpmn/definition_release_test.go`
    for the scheduled-cutover delete. Each was verified to fail with its fix reverted.
  - Still open, with reasons, in §6 of the doc: optimistic locking, message subscriptions,
    per-node Skip/Cancel actions, instance selection, resumable migration runs.

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
  - **Go SDK** (own module, zero dependencies): deploy/start, messages/signals,
    tasks, and a long-poll `Worker` whose handler budget is its lock. Proven by its
    `examples/quickstart` against a live server: login → deploy BPMN → worker serves
    the external task → human task claimed/completed → instance completed → timeline read.
    `docs/integration.md` documents exactly what that program exercises. Since extracted
    to its own repository, [gsoultan/metis-sdk](https://github.com/gsoultan/metis-sdk),
    where its own CI enforces the zero-dependency promise; it is no longer part of
    `make gate` here.
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
   that suddenly return nothing, then production, then the default. **2026-09-25:** the
   watching no longer needs the logs — `metis_strict_tenant_scope_enabled` and one
   `metis_strict_tenant_scope_denied_site` series per denied path are on the metrics
   endpoint, with an alert rule in `docs/strict-tenant-scope.md`. What remains is
   operational and needs a real workload: the staging soak, production for a release
   cycle, then deleting the flag.
2. ~~**P0 Reliability remainder**~~ — the connector contract tier landed
   (`tests/connector/contract_test.go`), which was the last missing tier. Outage
   simulation and feature flags had already landed.
3. ~~**P1** — `golangci-lint` backlog burn-down~~ — closed. `golangci-lint run` reports
   **0 issues**. The last seventeen were cleared rather than baselined: unchecked type
   assertions in `internal/pkg/lru` (now comma-ok, degrading to a cache miss rather than a
   panic), a dead `maxConcurrentJobs = 5` const whose comment still claimed it was the
   semaphore default while the real one is `defaultJobWorkers = 10`, a discarded static-asset
   write, `noctx` in four tests, and one genuine false positive — the shutdown drain's
   deliberately non-inherited context, now `//nolint:contextcheck` with the reason.
4. ~~**P2 UX Delight** — Task Inbox SLA fields~~ — already delivered; this entry was stale
   against the checklist at §9.7, which marks the overhaul done. Priority and due date are
   authored on the node, copied onto the task in `task.go`, and carried out through
   `TaskPBAdapter`. That chain had no test; `tests/bpmn/task_sla_fields_test.go` now asserts it
   end to end, because the failure would have been silent — every task ordinary, never overdue.

**Found while closing the above (2026-09-07), all fixed:** three cross-tenant disclosures of the
same shape — a filter that was *skipped* when absent rather than matching nothing, with no
tenant scope behind it. `GET /processes/statistics` counted every organization's instances and
tasks; `ListUsers` and `ListGroups` returned every account and group in the installation, with
memberships preloaded. Regression tests in `tests/tenant/statistics_scope_test.go` and
`tests/tenant/directory_scope_test.go`. Note these were invisible to
`METIS_FEATURE_STRICT_TENANT_SCOPE`: a query that never asks for a scope is not one the flag
can deny. Scoping them is what made them visible to it, which is why item 1 below gained three
call sites.

**Closed since this list was written:** Phase 2 landed the real FEEL parser, and the
memory-exhaustion vector it existed to remove is now off by default —
`javascript-conditions` ships `false`, so a default installation refuses `js:` conditions
outright rather than handing authored content to a runtime no sandbox setting can bound.
A gateway whose conditions are all refused raises an incident naming the gateway; it does
not guess a branch (`tests/bpmn/refused_js_condition_test.go`). Installations still
migrating can set `METIS_FEATURE_JAVASCRIPT_CONDITIONS=true`, and
`GET /api/v1/definitions/javascript-conditions` gives them the worklist.

---

### Verification coverage (2026-08-19)

Two gaps closed, both of the same shape — a test existed and had never run:

> **Superseded 2026-09-18.** This section is a record of what was true on the
> date above; it is left as written. Two things in it no longer hold: the
> product is PostgreSQL-only (`config.DriverPostgres` is the only driver), so
> there is no `dialects` job and no MySQL or SQL Server to prove — the
> PostgreSQL and RabbitMQ suites run on `go-security-reliability`, which still
> fails on any skip. And GitHub Actions is running again; the blocker below is
> resolved.

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
