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
   - Visual role editor — the Roles tab on Platform access (#118).
   - Group/org-scoped access — organization-scoped: **done** (2026-09-27). A role is
     granted in one organization and acts only in requests for it; a role held in
     every organization is the platform administrators'. Group-scoped is not: a
     group's `roles` are stored and grant nothing.
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
        **Done 2026-09-26 (INT-15):** both start when `METIS_RABBITMQ_BRIDGES` or
        `METIS_RABBITMQ_CONSUMERS` names them, so the reconnect logic runs in a server
        that is configured to use it. Off by default; see the entry of that date below.
        **Corrected 2026-09-26 (`rabbitmq-hardening`):** `messaging_test.go` never had a
        reconnect test. Reconnecting is tested now: against a broker in memory
        (`messaging_bridge_test.go`, `messaging_consumer_test.go`,
        `messaging_reconnect_test.go`) and, in CI, against a real one through a proxy that
        drops confirms, refuses and cuts connections (`messaging_broker_test.go`).
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
        edit their own profile (#96); the visual role editor is the Roles tab on Platform
        access — who holds which role, ticked by an administrator, beside what each role
        is required for as the gates enforce it (`GET /api/v1/roles`, held to endpoints.go
        by a test; #118). That legend reads in the interface's language: its headings and
        its actions, which the catalogues word by method name, with the server's English
        for a method they do not know yet (2026-09-27); the role names and their sentences
        are still English. Organization-scoped access is built (2026-09-27, branch
        `organization-roles`): a role is granted in one organization, on the account's
        membership (migration 30), by that organization's administrators, and acts only in
        requests for it; a role held in every organization is changed only by a platform
        administrator (`METIS_PLATFORM_ADMINS`). Roles are still the four fixed in code,
        and group-scoped access is not built: a group's `roles` are stored and grant
        nothing.
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
  - [x] Phase 3 complete (`load/chaos`, `canary + hardening`, `playbooks/docs`). Load/chaos:
        #103. Hardening: key rotation (#91) and the fixes since. Playbooks: the
        runbooks (`docs/runbooks.md`), held to the alerts by a drift test. Canary
        (decided 2026-09-26: a deployment-level canary, while flags stay
        installation-wide): `deploy/kubernetes/canary.yaml`, judged by two alerts
        that compare its 5xx and read latency with the stable track's, with the
        procedure in the runbooks and `tests/drift` holding its pod to the stable one.

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
    docs. It waits for that decision. **Decided and done 2026-09-26:** wired up behind
    configuration, off by default — see the entry of that date below.
  - **Found and not fixed — for the backlog:**
    - A webhook signature covers the body only. The delivery-ID header is unsigned and
      nothing is timestamped, so a captured delivery can be replayed under a new ID.
      Closing it needs senders to sign a timestamp as well. **Closed 2026-09-26** by v2
      signatures (branch `webhook-replay`; entry below).
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
    drafts kept separately. It waits for that decision. *Decided and done 2026-09-26: see
    that date's "decision versions" entry.*

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
      counted. **Fixed on `environments-live` (entry below).**
    - No broker/DLQ runbook: the consumer it would cover is never started (INT-15).
      **2026-09-26:** the consumer now starts when configured. Still no runbook, because
      still no alert: nothing measures a bridge's or consumer's connection, and
      `tests/drift` ties runbooks to alerts. Until a metric exists, what is logged at
      start and on each reconnect, and when a message is dead-lettered, is in
      `docs/integration.md`.

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
  - **Was open, done 2026-09-26** (branch `complete-reads`, one commit per fix, each with
    a test past 1,000 rows that failed first). Guards: the last-administrator guard of an
    organization (asked of the database, not a member list) and of the installation (it
    read the account's grants from a capped list of every grant, and let the last
    administrator go; a deleted administrator also still counted). Walks: the decision
    delete guard and message and signal start events (every version of a project), the
    tenant scope itself (an organization with over 1,000 projects lost the rest), a
    directory sync's deactivation and a participant's removal, the backfills v2 and v3,
    an incident's external-task re-offer and a migration's repeated hold. Views and
    exports: an instance's audit trail and execution path, the OCEL export, users, group
    members and groups, platform accounts, projects and organizations, sub-processes,
    incidents, the version history and the live-version marks. Paging: every paged list
    orders by creation time and then id. The step heat map was already a grouped count
    over every running token (984038e).
  - **Was open, done 2026-09-26** (branch `notifications-paged`, one commit per change,
    each test failing first): a person's notifications were the newest 1,000 and the bell
    counted unread among them. The bell's number is a server-side COUNT now
    (`GET /api/v1/users/me/notifications/unread-count`, 20 of 1,050 where it said 0), and
    it is all the bell polls; the list reads a page at a time, newest first, ordered by
    creation time and then id (`GET /api/v1/users/me/notifications?page=&page_size=`,
    1,050 walked exactly once where the list reached 1,000), and offers older pages. Both
    take the person from the session and scope in the query. Migration 29 indexes both
    (unread count 1,797 buffers to 4 at a million rows). The older
    `GET /api/v1/notifications?user_id=` is unchanged for other clients.
  - **Still open**: ~~The OCEL export never names a case's process version
    (the instance it reads carries only the definition id), at any size.~~ *Done
    2026-09-26: see that date's "audit order" entry.* ~~The audit
    trail is read in one statement rather than keyset-walked: the entries one
    transaction writes share its created_at and their ids are random, so their order
    rests on PostgreSQL returning ties as written; writing audit ids as UUIDv7 would
    make it explicit.~~ *Done 2026-09-26, with a sequence rather than UUIDv7: see that
    date's "audit order" entry.* Unreached reads that still stop at 1,000:
    Task().List/ListByProject/ListByAssignee, Decision().List/ListByProject,
    deployments, forms, variable snapshots and compensatable activities by instance.

- 2026-10-04 (completed): an administrator can waive, cancel or hold one instance in place
  (P0 reliability & audit) — the second part of slice 3 of the approval-adjustments work
  (3a-2). Branch `in-place-waive`, from `migration-rewrites-what-is-open` at `4aa14e7`; no
  schema migration. Driver: bpm for the acts and the engine, arch for the route ·
  Challengers: sec, go, test, perf, po.
  - **Problem.** One running instance could be dealt with outside its process only by
    deploying a second version and migrating it with a `skip`, a `cancel` or a `hold`. An
    instance that was `active` with nothing left could not be closed at all. And the effects
    a migration's decisions share had defects of their own, found while they were being made
    callable for one instance: a skip of an approval several people give ended one run and
    left the rest; a skip and a cancel recorded a task as nobody's while taking it from
    somebody who had just claimed it; a cancel left parked work on offer and incidents open,
    and a worker's late report then moved the cancelled instance on.
  - **What changed.**
    - *The command.* `POST /api/v1/instances/{id}/deviations`, `kind` `waive`, `cancel` or
      `hold`, for administrators of the organization the request is for, at the endpoint and
      again at the service. A dry run unless the body says `"dry_run": false`. A preview
      answers the plan with every refusal that is true, the warnings and a `visit_key`; an
      apply names the key and is decided on the row its lock returned: replay first, then
      still running, then the plan made again from that row with the same key and no
      refusal, then still waiting where it acts; the act, its ledger row (`origin:
      in_place`) and its trail entry in that one transaction.
    - *A waive* ends a user or manual task without anybody performing it, recorded as
      waived: the tasks `canceled`, each holder told, `node_skipped` with `outcome: waived`,
      no completion announced. Its outputs are limited to what the form of every open task
      declares, whatever `METIS_ALLOW_UNDECLARED_TASK_VARIABLES` says, and every place in
      the definition that decides from a field the step declares must be given its value.
    - *A cancel* ends the whole instance, whichever step it names, and with no step closes
      an instance that waits at no step.
    - *A hold* raises one incident, or uses the one open on the step, and changes nothing
      else; its key covers the step's incidents, so a step can be held again once the
      incident is resolved.
    - *One implementation of the three effects* (`nodeActions`), which the migration's
      `skipNode`, `cancelInstance` and `holdInstance` now call under the locks and guards
      they had. A skip of a user or manual task ends the step whole
      (`Engine.FinishActivity`). A skip and a cancel hold the row of each task they
      withdraw, after the instance and in id order, and record and announce from that row.
      A cancel withdraws parked external tasks and closes the instance's open incidents; a
      migration's cancel records the two counts when they are not zero.
    - *An instance that has ended stays ended.* A worker's report on one is refused and its
      parked work removed; a queued call for one is settled without calling; a job that
      fails for one raises no incident. These three commits (`ef6243d`, `e9bde36`,
      `4633531`) are also PR #146 against `main`, where the defect has been since 0.4.0;
      whichever merges second carries no diff for them.
    - *A gateway with no way out fails with a typed error* (`entities.NoFlowSelectedError`),
      its text byte for byte what it was, so a waive that cannot advance is answered 400
      naming the gateway and an error boundary event still catches it.
  - **Acceptance criteria**, each with the test that holds it (`tests/bpmn` unless named):
    1. *In place, no second version* (A6) — `TestAWaivedStepIsWithdrawnItsHolderToldAndTheInstanceMovesOn`,
       `TestCancellingInPlaceEndsTheInstanceAndWithdrawsItsWork`,
       `TestHoldingInPlaceRaisesOneIncidentAndChangesNothingElse`.
    2. *Dry run by default; only the boolean `false` in the field named so applies* (A7) —
       `TestOnlyARequestThatSaysDryRunFalseChangesAnything` (24 near-misses, every table
       unchanged after each) and `TestADeviationRequestThatCannotBeReadIsRefusedInPlainWords`
       (`tests/deviation`); `TestAnApplyWithoutThePreviewsVisitKeyIsRefused`;
       `TestAPreviewChangesNothingAndWaitsForNobody`.
    3. *A reason is required* (A8) and *a waive is refused where it would guess* (A9): a
       step with other than one way out, anything but a user or manual task with open work,
       a step reached more than once at once — `TestAWaiveIsRefusedWhereItWouldGuess`,
       `TestEveryRefusalSaysWhatIsWrongInWordsSomebodyCanActOn`,
       `TestAWaiveOfAStepReachedTwiceAtOnceIsRefused`.
    4. *A repeating approval is ended whole* (A10) —
       `TestWaivingAParallelApprovalWithdrawsEveryOpenRunAndAdvancesOnce`,
       `TestWaivingASequentialApprovalStartsNoFurtherRun`; through a migration,
       `TestASkippedRepeatingApprovalLeavesNoRunBehind` (`tests/instancemigration`).
    5. *Embedded, ad-hoc and called* (A11) — `TestAWaiveInsideAnEmbeddedSubProcessMovesOnInsideIt`,
       `TestAWaiveInsideAnAdHocSubProcessRereadsItsCompletionCondition`,
       `TestAWaiveInACalledProcessResumesItsCaller`,
       `TestAWaiveInACalledProcessSaysItsCallerWasNotRead`,
       `TestAWaiveInACalledProcessLetsItsCallerDecideOnAValueItAlreadyHolds`.
    6. *Recorded as waived, never as an approval* (A12) — criterion 1's first test: no
       `TaskCompleted`, no `task_completed`, `outcome: waived`.
    7. *Only fields the form declares* (A13) — `TestAWaiveSetsOnlyWhatTheStepsFormDeclares`
       (with the escape hatch on), `TestAWaiveMaySetOnlyWhatEveryOpenRunsFormDeclares`.
    8. *Every decision point that reads the step is listed, and a missing value refuses*
       (A14) — `TestAWaiveMustSayWhatItCountsAsForTheGatewayAfterIt`,
       `TestAValueLeftFromAnEarlierVisitDoesNotCountForTheGateway`,
       `TestThePlanListsEveryKindOfDecisionPointThatReadsTheStep`;
       `TestDecisionPointsReading`, `TestADecisionPointIsListedWhereverTheInstanceCouldMeetIt`,
       `TestWhatTheEngineDoesNotEvaluateIsNotADecisionPoint` (`services/impl`);
       `TestReferencedNames`, `TestDecisionTableReads` (`logic`).
    9. *A retried request never acts twice* (A15) — `TestTwoAppliesOfOnePreviewWaiveOnce`,
       `TestAppliesOfOnePreviewSentTogetherWaiveOnce`, `TestTwoAppliesOfOnePreviewCancelOnce`,
       `TestTwoAppliesOfOnePreviewHoldOnce`, `TestADifferentRequestForAVisitAlreadyActedOnIsRefused`,
       `TestARetryOfAWaiveAnswersItsRowWhateverTheInstanceHasBecome`,
       `TestAWaiveOfAStepThatMovedSinceThePreviewIsRefused`,
       `TestACompletionThatArrivesWhileItsStepIsBeingWaivedIsRefused`; `TestDeviationVisitKey`
       and its four neighbours (`services/impl`).
    10. *Administrators only; other organizations see nothing* (A17) —
        `TestOnlyAnAdministratorOfTheOrganizationDeviatesAnInstance` (seven callers, three
        kinds, preview and apply, whole bodies, every table unchanged),
        `TestSomebodyWhoMayNotDeviateIsToldNothingElse`,
        `TestAnInstanceOfAnotherOrganizationIsAnsweredAsOneThatIsNotThere` (`tests/deviation`);
        `TestTheServiceRefusesAnyoneButAnAdministrator`,
        `TestAnApplyByAnybodyButTheOrganizationsAdministratorChangesNothing`;
        `TestMakeEndpoints_AdministrativeEndpointsAreRoleGated`, `role_legend_test.go`
        (`endpoints`), `tests/endpointwiring`, `tests/roledrift`.
    11. *Statuses are real, through the route*: 200 for a plan, refused or not, an apply and
        a replay; 400 for a malformed request, an apply the plan refuses and an apply that
        comes too late; 500 for the server's own failure whatever its words —
        `TestAMalformedDeviationRequestIsA400ThatSaysWhatToFix`,
        `TestARefusedPlanIsA200ToPreviewAndA400ToApply`,
        `TestAnApplyThatComesTooLateIsA400ThatSaysWhatHappened`,
        `TestAnApplyOnASuspendedInstanceIsA400ThatSaysItIsSuspended`,
        `TestAWaiveAGatewayCannotFollowIsA400ThatSaysWhoseGatewayItWas`,
        `TestAFailureThatIsTheServersIsA500WhateverItsWordsSay`,
        `TestTheReplyToADeviationHasOneShapeWhateverItHolds`,
        `TestANumberAWaiveCountsAsIsTheNumberTheGatewayCompares` (`tests/deviation`).
    12. *A cancel is the whole instance, shown and keyed whole* —
        `TestACancelShowsAndKeysEverythingItWouldWithdraw`,
        `TestACancelOfMoreWorkThanAPlanListsWithdrawsAllOfIt`,
        `TestACalledInstanceIsCancelledAloneAndItsCallerOnlyAfterIt`,
        `TestACalledInstanceWhoseCallerHasEndedIsCancelledWithNoWarningOfIt` (a called
        instance is cancelled where it waits, with a warning while its caller has not
        ended; the caller is refused until what it called has ended).
    13. *An instance with nothing left can be closed* (rulings addendum §10) —
        `TestCancellingInPlaceClosesAnInstanceThatHoldsNothing`,
        `TestACancelThatNamesNoStepClosesAnInstanceThatWaitsNowhere`,
        `TestCancellingInPlaceWithdrawsATaskTheInstanceNoLongerWaitsFor`,
        `TestAStrandedCalledInstanceAndItsCallerCanBothBeClosed`;
        `TestAnInstanceWithNothingLeftIsClosedOverTheRoute` (`tests/deviation`).
    14. *A cancel leaves nothing that can move the instance* —
        `TestACancelWithdrawsTheWorkParkedForWorkers`, `TestACancelClosesTheIncidentsOpenOnTheInstance`,
        `TestCancellingInPlaceTakesTheWorkParkedForWorkers`,
        `TestNoCallIsMadeForAnInstanceCancelledInPlace`,
        `TestCancellingInPlaceClosesTheIncidentsOnTheInstance`;
        `TestAWorkerReportingOnAnInstanceThatHasEndedIsRefused`,
        `TestNoCallIsMadeForAnInstanceThatHasEnded`,
        `TestACallThatFailsAsItsInstanceEndsRaisesNoIncident`.
    15. *The record names who held the work when it was taken* —
        `TestAWaiveRecordsWhoHeldTheWorkWhenItWasTaken`,
        `TestACancelRecordsWhoHeldTheWorkWhenItWasTaken`;
        `TestAClaimRacingASkipIsRecordedAsItWasAnnounced`,
        `TestAClaimRacingACancellationIsRecordedAsItWasAnnounced` (`tests/instancemigration`).
    16. *A hold can be made again* — `TestAHoldCanBeMadeAgainOnceItsIncidentIsResolved`,
        `TestAHoldOfAStepAlreadyHeldUsesItsIncidentAndIsStillRecorded`.
    17. *A plan has a size whatever the process* —
        `TestAPlanListsAHundredDecisionPointsTheOnesToActOnFirst`,
        `TestAPlanListsTwoHundredOpenTasksAndKeysThemAll`,
        `TestThePlanNamesEveryMissingValueAndSaysWhenOneWaiveCannotSetThemAll`;
        `TestADefinitionThatRepeatsItselfIsReadInProportionToItsSize`,
        `TestAPlanStaysSmallWhateverTheProcess` (`services/impl`).
  - **What it must not have changed.** The 106 tests `tests/instancemigration` had pass
    unedited: the package has 111, the five new ones in three new files, and nothing else
    in it changed (`git diff --stat 4aa14e7 -- tests/instancemigration`). Slice 1's
    same-as-main suite (`tests/bpmn/repeating_shapes_unchanged_test.go` and `_pins_test.go`)
    is not edited. A completion sets what it set
    (`TestACompletionMaySetExactlyWhatTheTasksFormIsSaidToDeclare`, `services/impl`). An
    error boundary event still catches a gateway with no way out
    (`TestAGatewayThatCannotChooseFailsInWordsAnErrorBoundaryCatches`). A migration's
    cancel of an instance with nothing parked and no incident open records what it recorded
    (`TestACancelOfAnInstanceWithNothingParkedSaysNothingOfIt`).
  - **Rulings.**
    - *The whole definition is read, not what follows the step.* A walk along the flows is
      right only while it copies every way the engine moves a token, and it missed four.
      The cost is being asked for a value only a place already passed would read.
    - *Every cancel shows and keys the whole instance.* A cancel naming a step withdrew
      every open task and listed only that step's.
    - *Only what was looked at is said.* The warning for an instance that waits nowhere
      does not say nothing will move it on: its jobs and waiting events were not read.
    - *A repeating approval is ended whole, and a step that runs once and was reached
      several times at once is refused*: a waive that moved on fewer times than completing
      each task would is a branch dropped without a word.
    - *A caller upstream and a called process downstream are warned of, not refused.* A
      plan reads the definition its instance runs.
    - *A gateway with no way out during a waive is the caller's to hear about* (400, by a
      typed error, never by matching text), and only when it is the whole of what failed.
    - *The body is decoded before the gate*, as on every route: a signed-in account that is
      not an administrator gets the 400 for a body that cannot be read.
    - *The in-place cancel's row names at most 200 tasks*; the migration's row is not
      changed, because its rows are pinned.
  - **Lock order.** Instance, then task rows by id, then external-task rows, everywhere:
    the in-place apply, a migration's decision, a completion and a worker's report take
    the instance first. A hold takes the instance only. A preview takes nothing.
  - **What it costs.** Counted from the code, not measured. A preview reads the instance,
    its definition and its whole task list; for a waive each distinct form once and at most
    64 decision tables; for a cancel the processes it called, its parked work and its
    incidents; for a hold its incidents. An apply locks the instance, looks for the visit's
    row, makes the plan again, takes one `FOR UPDATE` per open task it withdraws, and writes
    one ledger row and one trail entry. Every service job reads its instance once more
    before its call, without a lock; a job that fails for the last time locks it once.
  - **Upgrade.** No migration. `docs/upgrading.md`, *An instance a migration left with
    nothing to do*: the unsupported `UPDATE` is replaced by the cancel in place.
  - **What a client meets that it did not** (`CHANGELOG.md`). The new route. On a migration:
    a skip of a repeating approval clears the step in one run; a skip or a cancel waits for
    a claim in flight; a cancel's row and entry carry `external_tasks_withdrawn` and
    `incidents_closed` when there were any, and the instance's incidents read `resolved`.
    For a worker: a report on work of an ended instance is refused, HTTP 200 with the
    refusal in `error`. The generic sentences `narrativeFor` keeps for `instance_cancelled`
    and `instance_held` no longer say "by a migration"; every writer of those entries gives
    a sentence of its own, so no reader meets them.
  - **Not in this slice.** A second approver for a waiver (3b). A screen: the command is
    made through the API. A Connect or gRPC call. Recording who released a hold: resolving
    an incident still writes nothing. Closing one task of an instance. A deadline on the
    server for an apply that waits.
  - **Found, not changed.** Each says how it is known: *probe* (run once and deleted),
    *run* (a test in the tree shows it), *read* (from the code, not run).
    - **`EndEventHandler.resumeParent` writes the caller from an unlocked read** (`GetInstance`,
      then `UpdateInstance`). A called process that ends while its caller is waived, or
      completed by hand, at a parallel step puts the caller's token back on the finished
      step and leaves the next step's task with no token. *Probe*; on `main` too. A waive on
      a caller holds the caller's row longer than a completion by hand, so each one widens
      the window. P0 reliability, its own PR. The same function resuming a cancelled caller
      (the ledger entry's item) cannot be reached through the cancel in place, which is
      refused around a called instance that has not ended, and is still open through a
      migration's cancel (*read*).
    - **A step that runs once and is reached twice at once** (two flows of a parallel fork
      entering one user task) holds two tokens and two tasks. The first completion takes
      both tokens and moves on once; the second task is then completed with no token under
      it and moves on again (*probe*, and *run*: `TestAWaiveOfAStepReachedTwiceAtOnceIsRefused`
      finishes both by hand). With an end event after the step, the first completion
      completes the instance with the second task still open, and nothing closes that task:
      a cancel of a completed instance is refused (recorded by Task 5; the ledger does not
      say it was run). A migration's skip of such a step advances once (*read*).
    - **Work of an instance that ended on its own can still be fetched.** The engine does not
      withdraw parked work when an instance ends other than by a cancel: at a terminate end
      event, or an end event reached with work still parked. A worker fetches it, does it,
      and is refused when it reports (*run*: `TestWorkFetchedAfterItsInstanceEndedIsRefusedAndRemoved`).
      The fetch has no predicate on the instance's status: its query is generated, for
      three dialects.
    - **A worker's report makes no token check on a running instance.** A report on a step
      an interrupting boundary event already left still advances from that step (*read*).
      A worker whose lock has run out is refused before the instance's status is asked,
      so the row of an ended instance's work stays and is offered again (*read*).
    - **The check before a call asks only whether the instance has ended.** A queued call
      for a step its running instance has already left is still made; asking for the token
      too would move `TestADeadlineOnAServiceCallIsUnchanged` (*read* from the pin). The
      read decodes the whole instance row and is not benchmarked.
    - **The worker routes answer HTTP 200 with the refusal in `error`** (*probe*, over HTTP;
      Connect and gRPC not run).
    - **A suspended instance.** A worker's report and a queued call proceed for one as
      before. Nothing in the product suspends an instance or resumes one (*read*); the
      in-place command refuses one as suspended and no longer says to resume it (*run*:
      `TestAnApplyOnASuspendedInstanceIsRefusedAsSuspended`). A called instance whose
      caller is suspended is still warned to "cancel or hold that one next", which the
      command then refuses (*run*: `TestACalledInstanceOrACallerThatIsSuspendedHasNotEnded`).
    - **Message correlation keys read process variables.** A value left by an earlier visit
      correlates silently; it is not a decision point of a waive's plan (*read*).
    - **A value derived from the step's is not traced.** A script, or a service task's
      input mapping, that computes another variable from a field the waived step sets is
      not followed to the gateway that reads that variable (*read*, and
      `TestWhatTheEngineDoesNotEvaluateIsNotADecisionPoint`).
    - **Nothing at deploy bounds a form's field count, how many nodes share an id, or a
      name's length.** Duplicate node ids deploy. The plan is bounded against each; the
      definition validator should refuse them (*read*). Where nodes share an id a point's
      `reads_in_all` and `missing_in_all` are sums, and can count a name twice.
    - **Resolving an incident takes no lock on the instance, asks nothing of its status and
      writes no trail entry** (*read*). So a hold's release is unrecorded, an incident a
      hold found open can be resolved a moment later, and an incident on an ended instance
      can be resolved. A hold's incident outlives its instance completing normally
      (*read*).
    - **The inbox words a hold as "<step> failed" with "Try again"**, and `explainIncident`
      reads a technical cause out of the administrator's reason (*read*:
      `IncidentInbox.tsx`, `domain/incidents.ts`). A hold on a service or external step may
      use the engine's own failure incident, where "Try again" retries the call.
    - **`withdrawOn` and the engine's `cancelOpenTasksOn` withdraw from an unlocked read.**
      The holder window closed here for a waive and a cancel is open for a skip of a step
      that is not a user or manual task, and for a task a boundary event or a met
      completion condition withdraws (*read*).
    - **No scoped read of an instance's open tasks exists.** Every plan and every apply
      reads the instance's whole task history and filters it (*read*).
    - **A gateway refusal answered 400 is still logged at Error level**, once per frame, in
      `NodeHandlerTemplate.Execute` (*read* by the review).
    - **The migration's cancel row grows with the open task count**: two entries a task, in
      one row, with no cap (by arithmetic, not measured). `withdrawParked` fails the
      cancel when the instance's definition will not load (*read*).
    - **A queued job of a cancelled instance stays `pending` until its time comes** (*read*),
      and is then completed without calling (*run*:
      `TestNoCallIsMadeForAnInstanceCancelledInPlace`). A pending timer stays too, and does
      nothing when it comes due (*read*).
    - **No event says an instance was cancelled or held.** Only `TaskCanceled` for each task
      withdrawn (*run*: the no-step cancel and the hold raise none).
    - **Cancelling a called instance leaves its caller waiting.** The cancel resumes nobody;
      the plan warns of a caller that has not ended, and the caller has then to be cancelled
      or held by hand (*run*: `TestACalledInstanceIsCancelledAloneAndItsCallerOnlyAfterIt`).
      Nothing ends a called instance when its caller ends early: slice 1's item, a process
      called from a step that has ended keeps running, is still open, and such an instance
      is now closable with the cancel in place (*run*:
      `TestACalledInstanceWhoseCallerHasEndedIsCancelledWithNoWarningOfIt`).
    - **An instance holding a token on a step its version lacks is closable in place, and
      nothing more**: a cancel may name that step and shows it by its id; a waive and a hold
      of it are still refused, and `docs/upgrading.md` says to migrate it back for those
      (*run*: `TestACancelCanNameAStepTheInstancesVersionNoLongerHas`, on a row written
      through the repository, since no release after 0.4.0 creates the state).
    - **A step with no way out leaves its instance `active` with nothing left once it is
      completed**, because the engine ends an instance only at an end event. Closable now
      with the cancel in place, not prevented (*run*: the fixture of
      `TestCancellingInPlaceClosesAnInstanceThatHoldsNothing`).
    - **No supported way closes one task** (the entry below's item): a waive ends every
      open task of its step, a cancel the instance.
    - **The route.** `Idempotency-Key` on it is not tested: read from the interceptor, a
      preview and its apply under one key get a 409, and the first answer of any status
      is replayed for 15 minutes. Duplicate names inside `outputs` keep the last. Whole
      numbers past 2^53 lose precision, as on the completion route. No route has a deadline
      on the server, and an apply waiting on a lock holds one of the 128 in-flight slots.
      A caller instance that cannot be found gives no warning and the apply then fails as
      a 500. A 400 carries no machine-readable code (all *read*).
    - The reason ends its sentence with two full stops when it ends with one itself.
    - The migration dialog does not show `passed_over` (the ledger entry's note). Connect
      and gRPC have no deviation call. There is no approval screen (P2).

- 2026-10-04 (completed): a migration rewrites only what is open, and only what it planned for
  (P0 reliability). Branch `migration-rewrites-what-is-open`, stacked on
  `migration-lands-or-passes-over` at `d8c6e19`; no schema migration. Driver: bpm ·
  Challengers: go, test, arch, sec.
  - **Problem.** Five defects in the instance-migration service, found by the review of the
    change below and present in 0.4.0 (read from `v0.4.0`): a mapping reopened completed tasks;
    an instance the plan never saw was moved without its control acknowledged; an instance
    already moved was rewritten again; a decision on a boundary event was accepted and never
    taken; a timer that had fired refused a migration. And four more that the review of this
    change reproduced in the same functions, fixed here by the owner's ruling: after a rename
    the submitter could approve their own request; a redirect made a control read as passed; a
    decision on a sub-process was accepted and never taken; a boundary event mapped onto a
    step completed the step by timer. The upgrading query this change first shipped read an
    encrypted column and found nothing.
  - **Root causes**, one each. (P1) The rewrite applied the mapping to every task and job of
    the instance whatever its status, and a task that changes step is rebuilt and offered.
    (P2) The plan and the apply each listed the instances, and the apply acted on its own
    list. (P3) Under the lock the instance was asked whether it is running, never whether it
    is still on the source version. (P4) The planner asked whether a decided node exists, never
    whether an instance can wait at it, and excused whatever sits on a decided node from
    landing. (P5) The landing check counted job rows whatever their status. (I1) Separation
    of duties reads completed tasks by step id, and a finished task kept the id it was done
    under when a mapping renamed the step. (I4) The completed-steps list was rewritten through
    every mapping, a redirect included. (I2) The refusal named two kinds of node where the
    rule is every node the engine leaves no token on. (I3) A mapped boundary event was checked
    only when its target was one too. (C1) The query read `audit_logs.data`, which is sealed.
  - **Acceptance criteria**, each with the test that holds it (`tests/instancemigration`):
    1. *A mapping rebuilds only what is still open.* A completed task and a cancelled task
       are the same row afterwards in every column but the step's id, which follows a rename;
       a timer that fired and a resolved incident are the same row;
       the open task is rebuilt from the step it lands on; one open task per token —
       `TestAMappingDoesNotReopenAnApprovalThatWasGiven`, `TestAMappingChangesOnlyWhatIsStillOpen`
       (`finished_work_test.go`).
    2. *An apply moves only the instances its plan was made for.* One that started on the
       source version after the apply planned is untouched and in `passed_over`, asked through
       the endpoint; a second dry run holds on the control it had not passed; acknowledged, it
       is moved with its `control_waived` row —
       `TestAnInstanceThatStartedAfterThePlanIsNotMoved` (`not_planned_for_test.go`).
    3. *An instance another run already moved is neither rewritten nor decided.* One
       `instance_migrated` entry, where the first run put it, `passed_over` through the
       endpoint; no skip, cancel or hold of it —
       `TestASecondRunDoesNotMoveAgainAnInstanceTheFirstAlreadyMoved`,
       `TestADecisionIsNotTakenOnAnInstanceAnotherRunAlreadyMoved` (`already_moved_test.go`).
    4. *A decision that can never be taken is refused in the plan.* A skip, a cancel and a
       hold of a boundary event on its own: the dry run names the event and the step to
       decide, the apply is refused, nothing is written, and the message still does what the
       version it runs says — `TestADecisionOnABoundaryEventIsRefusedInThePlan`
       (`undecidable_test.go`); the same for an embedded sub-process, naming the steps inside
       it, and deciding the step inside is then taken —
       `TestADecisionOnASubProcessIsRefusedInThePlan` (`undecidable_kinds_test.go`); every node
       type the engine has, refused or accepted by where its handler leaves the token, and a
       type added later must be placed; and the walk that names the steps inside ends on a
       sub-process that is its own parent, on a cycle of parents, and on a definition nested
       5,000 deep or 20,000 wide, each under a deadline —
       `TestTheStepsInsideASubProcessThatIsItsOwnParentAreFound`,
       `TestTheStepsInsideSubProcessesThatAreEachOthersParentAreFound`,
       `TestTheStepsInsideADeeplyNestedSubProcessAreFoundAtOnce`,
       `TestTheStepsInsideAWideSubProcessAreFoundAtOnce` (`services/impl`),
       `TestADryRunOverASubProcessThatIsItsOwnParentAnswers`;
       `TestADecisionIsRefusedWhereNoInstanceEverWaits`,
       `TestEveryNodeTypeIsDecidedOnPurpose`, `TestABoundaryEventsRefusalSaysToNameItWithItsStep`,
       `TestABoundaryEventMayBeNamedWithTheStepItIsAttachedTo` (`services/impl`).
    5. *Under the lock, work left on a decided step the new version lacks keeps the instance
       where it is.* An open task or a waiting event with no token under it —
       `TestWorkLeftOnADecidedStepIsNotCarriedToAVersionWithoutTheStep`. The state is made by
       hand: the engine leaves neither behind.
    6. *A timer that fired is not work; one still running is.*
       `TestATimerThatAlreadyFiredDoesNotStopAMigration`,
       `TestATimerStillRunningOnAStepTheNewVersionLacksStillStopsTheMigration`
       (`finished_timer_test.go`),
       `TestAWaitingMessageOnABoundaryEventTheNewVersionLacksStillStopsTheMigration`.
    7. *Nothing is stranded.* `assertNothingIsStranded` runs in every test above in
       `tests/instancemigration` but the one case whose premise is a stranded row (criterion 5,
       an open task left with no token).
    8. *The reasons are words.* `TestAPassedOverInstanceIsToldWhyInWords` (`services/impl`)
       has the three new ones.
    9. *Finished work follows a rename and not a redirect.* After a rename the submitter is
       refused the approval, at claim and at completion, and the finished task differs only
       in its step's id — `TestAfterARenameWhoeverDidOneHalfOfAFourEyesCheckMayNotDoTheOther`;
       a redirect, of either shape, leaves a finished task untouched —
       `TestARedirectDoesNotMoveFinishedWork` (`renamed_step_test.go`);
       `TestOnlyAMappingToANewIdThatNothingElseMapsOntoIsARename` (`services/impl`).
    10. *A control an instance only waited at is still held after a redirect onto it.* The
        completed-steps list does not take the control; the plan warns; the next migration,
        which drops the control, holds, is refused until acknowledged, and writes the
        `control_waived` row — `TestAControlAnInstanceOnlyWaitedAtIsStillHeldAfterARedirectOntoIt`,
        `TestTheCompletedStepsFollowARenameAndNothingIsWarned` (`control_after_redirect_test.go`),
        `TestARedirectIsWarnedOfWhereWorkDoneOnTheStepWouldNotCount` (`services/impl`).
    11. *A boundary event is mapped only to a boundary event.* Refused in the dry run and the
        apply, nothing moved, and the deadline then does what its version says —
        `TestABoundaryEventMayNotBeMappedOntoAStep`; boundary to boundary on another step
        still gets the older refusal — `TestABoundaryEventIsMappedOnlyToABoundaryEventOnTheSameStep`
        (`services/impl`); the landing refusal says what works, and it does —
        `TestTheRefusalForABoundaryEventsWorkSaysWhatWorks`,
        `TestAnInstanceAtAStepWhoseDeadlineWasDroppedIsHeldAndMovedOnceItHasLeftTheStep`
        (`boundary_mapping_test.go`).
    12. *The upgrading query finds what it is for, on rows the server wrote.* Read out of
        `docs/upgrading.md` and run as printed over sealed rows: the reopened tasks listed with
        step, name, person and kind of mapping, the negatives not, and each put back by the
        page's `UPDATE` — `TestTheUpgradingQueryFindsTheTasksAnEarlierReleaseReopened`
        (`upgrading_query_test.go`).
  - **What it must not have changed**, pinned before each change it guards and passing
    unedited after: the four pins of the change below (`unmoved_pins_test.go`); a pending
    timer and a waiting event each move with their step, and a skip of a timer wait moves the
    instance and leaves the timer to be dismissed when due (`live_rows_pins_test.go`, three
    pins); a skip, a cancel and a hold naming an approval and the deadline on it
    (`decided_with_its_step_pins_test.go`). The 82 tests the package had pass unedited; it
    has 106. No existing refusal or warning text changed; the landing refusal gained a sentence
    after its own, for a boundary event. No pin's recorded text changed: none has a finished
    task on a renamed step.
  - **Rulings, and one decision still this change's own.**
    - *Finished work follows a rename and not a redirect* (the owner's ruling, on the review's
      finding). A rename is a mapping to an id that is not a step of the source version and
      that nothing else is mapped onto. Under it a completed or cancelled task takes the
      step's new id — one column, by `TaskRepository.RenameFinishedStep`, guarded by the
      status — and the completed and compensated lists follow. Under a redirect neither is
      written. This replaces the first form of this change, which left every finished task
      under its old id and lost the four-eyes rule across a rename.
    - *A boundary event may still be named with the step it is attached to* (kept by the
      review). Refused without exception, a migration that decides an approval with a deadline
      the new version also drops could not be planned. The better design is in *Found*.
    - *A timer left on a decided step does not keep the instance back.* A skipped timer wait
      leaves exactly that, and the engine dismisses it when due; held back for it, the
      instance could not move until the timer's hour. Pinned. The same exclusion covers a
      queued service call, where it is not harmless: see *Found*.
    - *An escalation throw is left decidable.* Its handler does not advance it itself, so it
      is not shown that no instance ever rests there.
  - **Lock order.** Unchanged: each decision in its own transaction (instance, then tasks),
    then the rewrite in another (instance first). New reads, none taking a lock: one `Get` of
    the instance after an action that did not act (`notDecided`), outside any transaction. The
    two new questions under the rewrite's lock, and the one under each decision's, are asked
    of the row the lock already returned.
  - **What it costs.** Counted from the code, not measured: nothing per instance in the
    ordinary case — the planned set is a map built once per apply; the version and the plan
    membership are comparisons on rows already read; the landing check reads what it read.
    One more read of the instance only when an action found it gone. For each finished task
    on a renamed step, one scoped read and one single-column `UPDATE`, in the rewrite's
    transaction. In the plan: the refusal for a sub-process indexes the definition's nodes by
    parent once and visits each node once, however deep or looped the definition is; the
    redirect warning reads each running instance's completed steps once.
  - **Upgrade.** No migration. `docs/upgrading.md`, *A task a migration reopened*, has the
    query for tasks 0.4.0 reopened, which reads only columns stored in the clear and is run
    by a test over rows the server wrote, and an unsupported `UPDATE` that puts one back from
    the trail: on its step's old id after a redirect, on the new one after a rename.
  - **Found, not changed:**
    - **A boundary event's work should ride with its step in the plan.** Today a migration
      that decides a step is refused for what waits on the step's boundary events unless each
      is named in a decision too, and that decision is a fiction: nothing is taken or recorded
      for it. The planner should excuse a boundary event's work when the step it is attached
      to is decided, and then refuse every decision on a boundary event.
    - **A waiting step mapped onto a node nobody waits at is still accepted.** The planner
      asks only that the node mapped to exists. A user task mapped onto a gateway, a script
      task or a start event would leave its token, and its rebuilt task, on a node whose
      handler never leaves one there. Read from `planFor` and `boundaryRefusals`, not run.
      The table of where an instance waits (`waitsAt`) is what a refusal would be built on.
    - **A step mapped onto a boundary event, and the start of an event sub-process mapped
      onto a step, are still accepted.** The neighbours of the mapping refused here. Read from
      `boundaryRefusals`, not run.
    - **Nothing limits how deep or how wide a definition may be, and nothing reads a node's
      parent when one is saved.** A sub-process that is its own parent deploys. This change's
      walk is bounded against it; the definition validator should refuse it. The engine is
      not bounded against it: `Engine.TriggerEscalation` climbs from a step to its parent in
      a loop with no record of where it has been, so an escalation thrown inside a
      sub-process that is its own parent, with nothing catching it, would never return. Read
      from the code, not run.
    - **A decision on the start of an event sub-process, and a boundary event on a
      sub-process, have no path.** The first is refused like any start event; the second
      cannot be named with its step, because a decision on the sub-process is itself refused.
      Both wait for the item above.
    - **After a redirect, a rule or a control on the step mapped to does not see work done on
      the step mapped from.** By design, and warned of in the plan. A durable per-instance
      record of the mappings applied would let a rule be asked across them; it needs a column.
    - **The rewrite writes back whole job rows it read earlier.** `Job().Update` sets status,
      lease and retries from the copy the rewrite listed. A job is settled outside the
      instance's lock when its work changes nothing else (a retry, a failure with its
      incident), so one settled between the rewrite's read and its write would be written
      back as it was: a failed job running again. Read from the code, not reproduced; the
      window is a few statements. A write of the two columns a migration means to change
      would close it.
    - **A skip does not take the step's queued service call off the queue.** `Proceed` leaves
      the job of the step it advances past. When the job runs the call is still made, and its
      result is dropped because no token waits (`executeServiceTask`); re-pointed at a version
      without the step, it fails for want of its node and ends as an incident. Read from the
      code, not reproduced. The landing check under the lock leaves every job on a decided
      step out, for the timer's sake; narrowing that to timers would keep such an instance on
      its version instead.
    - **No supported way closes one task.** A task 0.4.0 reopened can only be put back in the
      database (`docs/upgrading.md`). The adjustment of one instance in place, planned next,
      should be able to withdraw a task with a reason and a ledger row.
    - **An open incident is not re-pointed by a migration.** It keeps the version and the step
      it was raised on; resolving it queues its job, which is re-pointed. Read from the code.
    - **Work parked for an outside worker is neither moved nor checked.** `external_tasks` is
      not read or written by the migration, and completing one advances from the row's own
      step id. Read from the code, not run.
    - The reply to an apply through the endpoint carries the plan the endpoint made for the
      reply, not the one the apply made a moment later and acted on.
    - `escalated` is a task status nothing in the server sets; the migration treats it, as the
      landing check and the engine's withdrawal do, as not open.

- 2026-10-04 (completed): a migration never moves an instance onto a version that cannot run
  it (P0 reliability). Branch `migration-lands-or-passes-over`, from `instance-deviation-ledger`
  at `4bfdf89`; no schema migration. Driver: bpm · Challengers: go, test, arch.
  - **Problem.** A migration checked where an instance's work lands from its listing and not
    again under the instance's lock, so an instance that reached a removed step in between was
    re-pointed at a version without that step, and once its holder completed the task it stayed
    `active` for ever with no token and no task.
  - **Root cause.** "Can everything this instance holds land on the new version" was answered
    from a read taken before the instance's lock and never asked of the row the rewrite's lock
    returned; and which decision an instance gets was chosen from that same read.
  - **Acceptance criteria**, each with the test that holds it (`tests/instancemigration`,
    `lands_or_passed_over_test.go` unless named):
    1. *A migration that only moves work leaves alone an instance that reached, after the
       listing, a step the new version lacks.* Not rewritten, on its version with its token and
       task, nothing recorded, in `passed_over` with a reason naming the step and no id,
       `applied` false; a dry run afterwards is refused for that step; completing the step
       then advances it normally — `TestAnInstanceThatReachesAStepTheNewVersionLacksIsNotMoved`.
    2. *A decision reaches an instance that arrived at its step after the listing.* Skipped
       (advanced, a `waive` row, a `node_skipped` entry) and then moved; cancelled; held —
       `TestAnInstanceThatReachesASkippedStepAfterTheListingIsSkippedAndThenMoved`,
       `TestAnInstanceThatReachesAStepBeingCancelledAtAfterTheListingIsCancelled`,
       `TestAnInstanceThatReachesAStepBeingHeldAtAfterTheListingIsHeld`.
    3. *The rewrite is the safety net for what moves after that read.* An instance that
       reaches a decided step between the apply reading it again and the rewrite's lock is
       left alone and listed, and the same migration run again decides it, for a skip, a
       cancel and a hold — `TestAnInstanceThatReachesADecidedStepJustBeforeItsLockIsLeftForTheNextRun`;
       and when the new version has the step too, so that the work would land, because the
       question there is the decision and not the landing —
       `TestAnInstanceThatReachesAHeldStepBothVersionsHaveIsNotMovedUndecided`.
    4. *The reproduction from the ledger's review*, with a skip and with no decision at all:
       no token and no open task on the removed step on the new version, and the instance
       runs to its end — `TestTheStrandingReproducedInReviewNoLongerHappens`.
    5. *An instance is one row.* With two tokens of which one moved, it is left whole, or
       skipped at the step it reached and moved whole —
       `TestAnInstanceWithOneOfTwoTokensOnAStepTheNewVersionLacksIsNotMoved`,
       `TestAnInstanceWithOneOfTwoTokensOnASkippedStepIsSkippedThereAndMoved`.
    6. *A skip does not carry an instance onto a removed step.* When the step after the one
       skipped is missing from the new version too, or the skip leaves part of a repeating step
       behind, the skip stands and the instance stays on its version —
       `TestASkipThatLandsOnAStepTheNewVersionLacksLeavesTheInstanceOnItsVersion`,
       `TestASkipThatLeavesPartOfARepeatingStepBehindDoesNotMoveItToAVersionWithoutTheStep`.
    7. *No instance is left active with nothing to move it.* `assertNothingIsStranded` runs in
       every test above: each active instance has a token and an open task, each on a step the
       version it runs has; `runsToItsEnd` then completes what is open until the instance
       finishes.
    8. *The reasons are words.* Step names, never an id where there is a name —
       `TestStepNamesReadAsASentenceAndNeverByIDWhereThereIsAName`,
       `TestAPassedOverInstanceIsToldWhyInWords`,
       `TestUndecidedStepsAreTheDecidedStepsStillHoldingAToken` (`services/impl`).
  - **What it must not have changed**, with the tests that pin it, written and passing before
    the change (`unmoved_pins_test.go`): an instance that has not moved since the listing has
    the same result, tasks, ledger rows, incidents and trail entries, for a migration that only
    moves work, one that re-points it, a skip, a cancel, a hold, and a skip in a run where
    another instance is elsewhere — `TestPinAMappingOnlyMigrationOfAnUnmovedInstance`,
    `TestPinARepointingMigrationOfAnUnmovedInstance`, `TestPinADecisionAboutAnUnmovedInstance`,
    `TestPinASkipLeavesAnInstanceElsewhereToBeMoved`. The planner, the dry run, every refusal
    and warning text and the reply's shape are untouched, and the 67 tests the package had
    pass unedited.
  - **What does change for an instance that did not move by itself.** Three cases, each a skip
    that itself leaves the instance somewhere the plan was not made for. Two are criterion 6:
    the step after the skipped one is missing from the new version with no mapping and no
    decision, and a skip that leaves part of a repeating step behind; that instance used to be
    moved onto the new version with a token on a step it lacks. The third was found in review
    and has no test of its own: a skip that advances the instance onto another step the
    migration decides, one the new version keeps (a hold after a skipped step, say). It used
    to be moved there with that decision not made; it is now left on its version, listed, and
    decided by the next run (`undecidedSteps`, `migration_landing.go`, which asks only whether
    a decided step still holds a token).
  - **Lock order.** Unchanged: each decision in its own transaction (instance, then tasks),
    then the rewrite in another (instance first). The apply's extra read of the instance is
    outside any transaction, and the check under the rewrite's lock only reads.
  - **What it costs.** Counted from the code, not measured: the rewrite reads the instance's
    tasks, jobs and waiting events once more, in the transaction already open. A migration
    that decides work reads each instance before deciding it; the read it used to make after
    deciding is gone, so that count is unchanged.
  - **Upgrade.** No migration. `docs/upgrading.md`, *An instance a migration left with nothing
    to do*, has two queries for instances an earlier release stranded, each run over temporary
    tables holding rows it must and must not list.
  - **Found, not changed:**
    - ~~Skipping a repeating approval still counts one iteration and withdraws every task (slice
      1's entry). The instance is no longer moved with the leftover tokens; it takes one run of
      the same migration per remaining iteration to clear the step, and between runs it is
      `active` on its own version with tokens on the step and no open task.~~ *Done 2026-10-04:
      see that date's "waive, cancel or hold one instance in place" entry. A skip of a user or
      manual task ends the step whole, in one run
      (`TestASkippedRepeatingApprovalLeavesNoRunBehind`).*
    - Two skipped steps in a row are not skipped in one run: the second is found only by the
      next run, because a run decides from where the instance stood when it was read.
    - ~~Five defects in the same code path, none of them this change's and none the stale
      listing (P1 to P5 of this change's review).~~ *Done 2026-10-04: see that date's "a migration
      rewrites only what is open" entry, which fixes all five and says what it found in turn.*
    - ~~No supported way exists to close an instance that is `active` with nothing left: no
      route ends an instance, and a migration's `cancel` needs a token on the step it names
      (`:1384`). `docs/upgrading.md` says so and gives a direct `UPDATE` only as an unsupported
      last resort. An audited way to close such an instance is needed: the cancel of one
      instance in place, planned next, must work on an instance that holds no token.~~ *Done
      2026-10-04: see that date's "waive, cancel or hold one instance in place" entry. A
      cancel in place that names no step closes it, recorded
      (`TestCancellingInPlaceClosesAnInstanceThatHoldsNothing`), and `docs/upgrading.md` gives
      that in place of the `UPDATE`.*
    - The migration dialog still does not show `passed_over` (the ledger entry's note).

- 2026-10-03 (completed): an instance keeps a ledger of what was done to it outside its process
  (P0 audit) — the first part of slice 3 of the approval-adjustments work. Branch
  `instance-deviation-ledger`, from `cd7c4a3`, the head of `task-handover-accountability`,
  since merged to `main`; one
  commit per change, each with a test that fails without it. Migration 33. Driver: arch for the
  table, the route and the seam, go for the repository, bpm for the migration and the
  activation · Challengers: sec, go, perf; fe and ux for the timeline.
  - **Problem.** An administrator's override, a migration's skip, cancel or hold, and a step
    started inside an ad-hoc sub-process left at most an audit entry that an auditor cannot ask
    for by instance, and a migration's skip, cancel or hold could lose its entry while the change
    stood.
  - **Acceptance criteria**, each with the test that holds it (`tests/deviation` unless named):
    1. *The table.* Created as the model describes it, with no foreign key to the rows a
       hand-over and a completion lock, and with closed sets of kind, scope, origin and status —
       `TestMigration33CreatesTheLedgerAsTheModelDescribesIt`,
       `TestTheLedgerHasNoForeignKeyToTheRowsAHandOverAndACompletionLock` (`tests/migrations`),
       `TestDeviationKindsAreAClosedSetWithTheirReasonRule` and
       `TestDeviationScopesOriginsAndStatusesAreClosedSets` (entities).
    2. *Written only inside the change it records.* `TestADeviationIsRecordedOnlyInTheTransactionThatMakesIt`,
       `TestInTransactionIsTrueOnlyForAnOpenTransaction` (`repositories/db`); sealed, scoped to
       the organization and to the instance's project — `TestARecordedDeviationKeepsItsBusinessDataSealed`,
       `TestAnotherOrganizationNeitherWritesNorReadsAnInstancesDeviations`,
       `TestARowNamingAnInstanceOutsideItsProjectIsRefused`; a malformed row is a server error
       and leaves nothing — `TestAMalformedDeviationIsRefusedAndLeavesNoRow`,
       `TestPrepareDeviation` (impl); rows of one act read in the order written —
       `TestRowsWrittenInOneTransactionComeBackInWriteOrder`.
    3. *Reason.* Required for every kind but `control_waived` (which takes none) and
       `adhoc_activation` (optional); a person's missing or over-long reason is a 400, in plain
       words — `TestTheLedgerRefusesAReasonlessOverride`, `TestTheReasonRefusalIsPlainEnglishWithNoKindSlug` (impl).
    4. *Reading.* `GET /api/v1/instances/{id}/deviations` for any signed-in member of the
       organization, 401 for nobody, 404 for another organization's instance (administrator
       included), 400 for a malformed id, no account ids — `TestAMemberReadsAnInstancesDeviationsOldestFirst`,
       `TestTheDeviationsOfAnInstanceAreReadByItsOrganizationOnly`, `TestTheReadRouteReturnsNoAccountIds`
       (which looks for the account's id itself in the reply, not only for the key names it could
       travel under), `TestAnInstanceWithoutDeviationsReadsAsAnEmptyList`. A row says whether the
       server or an account made it (`actor_is_server`, true exactly when the row names no
       account), and `before`, `after` and `details` are always objects —
       `TestTheReadRouteSaysWhetherTheServerOrAnAccountActed`,
       `TestARowThatChangedNothingStillReadsWithItsThreeObjects`,
       `TestAViewSaysTheServerActedOnlyWhenNoAccountDid` and
       `TestAViewAlwaysCarriesItsObjectsAndWhoActed` (`endpoints/deviation`).
    5. *Hand-overs.* A row exactly when a reason was required, in the hand-over's transaction,
       and a hand-over whose row cannot be written is not made; a holder's own writes none; and no
       deadlock against a completion — `tests/task`: `TestEveryHandOverByANonHolderIsLedgered`,
       `TestAHolderHandingOnTheirOwnTaskWritesNoLedgerRow`, `TestOnlyAHandOverThatNeededAReasonIsLedgered`,
       `TestAnAdministratorHoldersOverrideIsLedgered`, `TestAHandOverThatCannotBeLedgeredIsNotMade`,
       `TestANonHolderHandOverInFlightIsNotDeadlockedByACompletion`.
    6. *Migrations.* A skip, a cancel, a hold and each accepted control loss write their rows
       with the run's id and name their entries; each is recorded once across reruns; one that
       cannot be recorded is not made, and nor is one whose entry cannot be written; a failure on
       the second instance leaves the first done and the rest untouched; a reason the ledger
       cannot hold is refused in the plan — `tests/instancemigration`:
       `TestASkipIsLedgeredWithItsRunAndItsEntry`, `TestACancelIsLedgeredAndItsEntryIsWrittenWithTheChange`,
       `TestACancelIsRecordedOnceAcrossReruns`, `TestAHoldIsRecordedOnceHoweverOftenTheMigrationRuns`,
       `TestAnAcknowledgedControlLossIsLedgeredPerInstance`,
       `TestOnlyTheInstanceThatHadNotPassedTheControlLosesIt` (an instance that had already
       performed the control step gets no `control_waived` row and is not told as having lost
       it), `TestADecisionThatCannotBeLedgeredIsNotMade`,
       `TestADecisionWhoseTrailEntryCannotBeWrittenIsNotMade`,
       `TestALedgerFailureOnTheSecondInstanceLeavesTheFirstDoneAndTheRestUntouched`,
       `TestAReasonTheLedgerCannotHoldIsRefusedBeforeAnythingMoves`, `TestAReasonAsLongAsTheLedgerTakesIsAccepted`.
    7. *Ad-hoc activation.* A row and a `step_activated` entry, written before the step runs and
       rolled back with it; an optional reason; nobody signed in is *System*; an account with no
       name is refused — `tests/bpmn`: `TestAnActivationIsLedgeredWithItsActorAndReason`,
       `TestAnActivationWithNobodySignedInIsLedgeredAsTheSystem`, `TestAnActivationThatCannotBeLedgeredIsNotMade`,
       `TestAnActivationByAnAccountWithNoNameIsRefusedAndNothingIsLeft`,
       `TestAnActivationWhoseEntryCannotBeWrittenIsNotMade`, `TestAStepThatFailsToStartLeavesNoRecordOfBeingStarted`,
       `TestAnActivationIsToldBeforeWhatTheStepThenDid`, `TestAnOverLongReasonIsRefusedBeforeAnythingIsReadOrStarted`;
       the route and its roles — `TestOnlyAnOperatorOrAdministratorActivatesAStepAndTheirReasonIsKept`,
       `TestAnActivationReasonTooLongIsRefusedAndNoneAtAllIsAccepted`; the timeline —
       `handOverNarrative.test.ts` and `TestAnActivationsEntryTellsWhoStartedWhatAndWhy` (impl).
    8. *Denials.* The read route as under 4; the activation route's 401, 403 and 404 as under 7;
       every new write path refuses a caller outside the organization as under 2.
    9. *A decision is made on the instance as its lock finds it.* A skip, a cancel and a hold
       each read the instance again under its lock and act only on one that is still running
       and still has a token on the step; one whose holder completed the step, or that
       finished, between the migration's listing and that lock is left as it is — nothing
       withdrawn, advanced, cancelled or raised, no row, no entry, not moved in that run — and
       the next run of the same migration moves it from where it stands —
       `tests/instancemigration`: `TestASkipOfAStepCompletedAfterTheListingDoesNothing`,
       `TestACancelAtAStepCompletedAfterTheListingLeavesTheInstanceRunning`,
       `TestAHoldAtAStepCompletedAfterTheListingRaisesNothing`,
       `TestASkipOfAnInstanceThatFinishedAfterTheListingDoesNothing`. Root cause: whether an
       instance was parked on the step was answered from a read taken before the lock and
       never asked again under it. This predates the slice; it is fixed here because the
       ledger would otherwise certify the second advance as a waiver. The reply to the apply
       names each instance left alone in `passed_over`, with a reason that names the step as
       people know it, and `applied` is false when the run passed instances over and acted on
       none — `TestAnApplyNamesTheInstanceItPassedOver`,
       `TestAnApplyThatPassedOverEveryInstanceSaysNothingWasApplied`,
       `TestAnApplyNamesAnInstanceThatFinishedBeforeItWasReached`,
       `TestAnOrdinaryApplyAndADryRunPassNothingOver`, and `TestAppliedIsSaidFromWhatTheRunDid`
       (`endpoints/definition`).
    10. *Only nobody signed in is the server.* A migration by a signed-in account with no
       username is refused before anything is planned, with the status an ad-hoc activation
       answers for the same account, where it used to be recorded as *System* —
       `TestAMigrationByAnAccountWithNoNameIsRefusedAsAnActivationIs`,
       `TestAMigrationNamesItsAuthoriserOrIsRefused` and
       `TestADryRunByAnAccountWithNoNameIsRefusedToo` (`endpoints/definition`),
       `TestAccountTellsNobodyFromAnAccountWithNoName` (`endpoints/principal`).
  - **What it must not have changed**, each with the test that pins it: a holder's reasonless
    hand-over writes no row and every audit sentence is as it was (the holder's tests under 5 and
    slice 2's `TestRecordEventLeavesEveryOtherEntryAsItWas`); a migration that only moves work
    writes no row (`TestAMappingOnlyMigrationLedgersNothing`); slice 1's same-as-main suite
    (`tests/bpmn/repeating_shapes_unchanged_test.go` and `_pins_test.go`), which this branch does not
    edit; migration 22's repair, whose test now leaves out a table a later migration creates
    (`TestMigration22RepairsEveryColumnTheReaderNeeds`), because a table created at 33 never had
    its columns made nullable and adding it to 22's list would change that migration.
  - **What it costs.** A hand-over or edit by somebody who does not hold the task, each
    migration decision and each ad-hoc activation do one more read (that the instance is in the
    project) and one insert, inside the transaction already open, after a check of the project
    that is cached for the request; each accepted control loss is one more read and one more
    insert, and an ad-hoc activation also writes a `step_activated` trail entry, which it had
    none of before. A skip's check that the instance is still on the step reads the row its lock
    already returned, and a cancel's and a hold's read nothing more. Counted from the code, not
    measured.
  - **What writes no row, by decision.** The ledger is for what somebody did to an instance
    that its process did not decide. Left out on purpose, each either modelled behaviour or
    already told by the trail, with one exception named below:
    - *Sending a message or broadcasting a signal.* The event the process was modelled to wait
      for. The trail shows the instance moving on, not who sent it; governing that channel is
      deferred to its own slice.
    - *An operator or administrator claiming a task nobody was named for.* The modelled
      fallback for such a task; the trail's claim entry names who took it.
    - *The engine withdrawing tasks* — approvals a met completion condition no longer needs, a
      task a boundary event interrupts, work parked for outside workers when an ad-hoc
      sub-process finishes. The process decided those; the trail says each was withdrawn.
    - *A holder's own hand-over or edit*, and *a migration that only moves work*, as pinned
      above.
    - *Resolving an incident*, the one a migration's hold raised included. It retries the
      failed work behind an incident and decides nothing about the process. This is the
      exception: it is not on the trail either (see *Found, not changed*). A `hold` row
      therefore records that the hold was placed, not that it is still open: no row is
      rewritten, its `after.incident.status` reads `open` for ever, and whether the hold is
      still open is the status of the incident it names, read from
      `GET /api/v1/incidents/{instanceId}`. Whether releasing a hold becomes a ledgered act is
      decided in the next slice, which adds the hold of one instance in place.
  - **Upgrade.** Migration 33 creates `instance_deviations`, backfills nothing, and waits two
    seconds for `projects` and `process_definitions` and stops, to be started again.
    `docs/upgrading.md`, *Migration 33: an instance's ledger of what was done to it*.
  - **What a client meets that it did not** (`CHANGELOG.md`). The new route; a `reason` on
    `POST /api/v1/processes/adhoc/activate`; a reason of more than 2,000 characters on a
    migration's skip, cancel or hold refused in the plan; a migration's skip, cancel or hold that
    cannot be recorded stops the run at that instance; a new `step_activated` audit entry and a
    `deviation_id` on the entries of the acts above; a migration's skip, cancel or hold that
    leaves alone an instance which left the step after the run listed it, on the version it is
    running; `passed_over` on the reply to a migration's apply, always present, listing each
    instance left alone and why; and `applied: false` for an apply that passed instances over
    and acted on none, where it said `true`.
  - **Not in this slice.** Waiving, cancelling or holding one instance in place, without a second
    version of its process, and a second approver. Both come in the next ones; the table already has
    the columns for them: nothing writes `approved_by` or `request_id`, and no row is anything but
    `applied`.
  - **Found, not changed:**
    - A control-loss row names the `instance_migrated` entry, which is written after the rewrite
      commits and, if it fails, only logged; the row can name an entry that does not exist.
    - A step name over 255 characters is shortened in the ledger row (the entry tells it in full);
      identifiers are never cut.
    - An account can still be named *System*: nothing reserves the name when an account is
      created or renamed. The route now tells such an account from the server
      (`actor_is_server`), but the trail's sentences and the `actor` column do not.
    - Resolving an incident writes no trail entry and names nobody, so nothing but the
      incident's own `status` and `resolved_at` says that a hold ended, and nothing says who
      ended it.
    - The migration dialog does not show `passed_over` yet. It reads `plan` and `applied`, so
      an apply that acted on some instances and passed others over is told as though every
      instance the plan counted was dealt with; one that passed every instance over is told,
      correctly, as *Nothing was moved*.
    - An apply that stops with an error part-way does not say which instances it had passed
      over before it stopped; the error names the instance it stopped at and how many had been
      dealt with, and the log names the ones passed over.
    - The ledger route answers 404 for another organization's instance where the audit route
      answers 200 with an empty list.
    - `details` is stored unencrypted, so a writer must not put a business value in it; today it
      holds a count, a sub-process id, an override marker and a control's compliance note.
    - ~~Skipping a repeating approval in a migration is still as slice 1's entry describes it
      under *Found and not fixed*: it counts one iteration and leaves the other iterations'
      tokens. The skip is now ledgered, not fixed; the in-place waive that follows is to end
      the whole step.~~ *Done 2026-10-04: see that date's "waive, cancel or hold one instance
      in place" entry. The waive in place and a migration's skip both end the whole step.*
    - `EndEventHandler.resumeParent` loads the parent and resumes it without looking at the
      parent's status, so a called instance that ends after its parent was cancelled, as a
      migration's cancel does, advances the cancelled parent. Read from the code, not reproduced;
      still open.

- 2026-10-03 (completed): hand-overs are checked and recorded (P0 security & audit) — slice 2
  of the approval-adjustments work. Branch `task-handover-accountability`, stacked on
  `mi-approvals-finish` at `c8250a3`; one commit per change, each with a test that fails without it.
  Migration 32. Driver: sec · Challengers: arch, test; ux and fe for the inbox.
  - **Problem.** Assigning or delegating a task took any name and asked for no reason, the
    trail named the person it went to as the one who acted, an edit overwrote three fields at
    once and was recorded as nothing useful, and a delegation had no owner and no way back.
  - **Acceptance criteria**, each with the test that holds it (`tests/task` unless named):
    1. *Target checks, deny by default.* An unknown name, another organization's member and
       somebody separation of duties bars are refused, and so is every hand-over whose step
       cannot be read; a task offered to people goes only to one of them, or elsewhere by an
       administrator with a reason, recorded as an override —
       `TestATaskCannotBeHandedToSomebodyWhoIsNotThere`,
       `TestATaskCannotBeHandedToSomebodySeparationOfDutiesForbids`,
       `TestATaskOfferedToPeopleIsHandedOnlyToOneOfThem`.
    2. *Reason.* Optional for the holder, required from anyone else, on assign, delegate,
       resolve, release and update — `TestHandingOnATaskThatIsNotYoursNeedsAReason`,
       `TestReleasingSomebodyElsesTaskNeedsAReason`, `TestTheHolderHandsTheirOwnTaskOnWithoutAReason`,
       `TestAnEditIsRefusedToAStrangerAndToAnAdministratorWhoDoesNotSayWhy`,
       `TestOnlyTheDelegateOrAnAdministratorWhoSaysWhyHandsATaskBack`.
    3. *Attribution.* The caller is the actor; target, previous holder, reason and an edit's
       before and after are beside it; the sentence reads correctly, and an entry that cannot
       be written stops the change —
       `TestTheTrailNamesWhoMadeAHandOverFromWhomToWhomAndWhy`, `TestTheTrailNamesWhoReleasedATask`,
       `TestAHandOverThatCannotBeRecordedIsNotMade`, `TestHandOverNarrative` (impl), and in the UI
       `handOverNarrative.test.ts`.
    4. *Partial update.* `PUT` changes only the fields present — `TestAnEditChangesOnlyTheFieldsItCarries`,
       `TestResendingAnUnchangedDueDateChangesNothing`, `TestAnEditSaysWhichFieldsItCarries` (endpoint).
    5. *Delegate is owner plus hand back.* — `TestADelegatedTaskGoesBackToItsOwnerWhoCompletesIt`,
       `TestDelegatingATaskNobodyHoldsIsRefused`, `TestAPendingDelegationIsHandedBackNotReleasedOrHandedOn`,
       `TestMigration32ReturnsADelegationWithNoOwnerToItsAssignee` (`tests/migrations`).
    6. *Notifications.* — `TestWhoeverATaskIsHandedToIsTold`, `TestADelegateIsToldTheTaskIsWithThem`,
       `TestAnOwnerIsToldTheirTaskIsBack` (observers),
       `TestTheOwnerOfAWithdrawnDelegationIsToldAsTheDelegateIs`.
    7. *Inbox.* — `TaskInbox.test.tsx` ("the table, for a delegated task"), `components/inbox/inbox.test.tsx`,
       `taskDelegation.test.ts`, and `locales.test.ts` for both languages.
    8. *Denials.* A caller who is neither holder nor administrator, and an administrator with
       no reason, on every changed endpoint — `TestAReasonDoesNotLetAStrangerHandATaskOn`, the
       tests under 2, and `TestAnOwnerSeesWhatTheyDelegatedAndNobodyElseDoes` for the new listing.
  - **Where the rule lives.** In the task service, on the row read `FOR UPDATE`
    (`task_handover_step.go`), which is what `docs/architecture-audit.md` 1.1 asked for; the
    endpoints pass who is asking and what they said. `TestOnlyOneOfSeveralSimultaneousHandOversByTheHolderWins`.
  - **What it must not have changed**, each with the test that pins it: a holder's release over
    Connect, which carries no reason (`TestTheHolderStillReleasesOverConnectWhichCarriesNoReason`);
    every other audit sentence (`TestRecordEventLeavesEveryOtherEntryAsItWas`); the events an
    assignment, an edit and a release raise (`TestObserversStillSeeTheEventsHandOversAlwaysRaised`);
    a claim separation of duties forbids, still a 403 (`TestAClaimSeparationOfDutiesForbidsIsStillForbidden`);
    a task's iteration through the adapters and the regenerated store, and a withdrawn task told
    it was withdrawn before anything about its delegation (slice 1's behaviour, asserted in
    `TestWhoATaskGoesBackToSurvivesTheStore` and `TestAWithdrawnDelegationLeavesItsOwnersList`).
  - **What it costs.** A hand-over reads more under the task's row lock: the account, the
    task's project, the step's definition (through the instance), the account's groups when
    the task is offered to groups, and, for a step with a separation-of-duties rule, the
    instance's tasks. 8 table reads before and 13 to 15 after were counted once during
    development from `pg_stat_user_tables` with a throwaway test that is not in the
    repository. A hand-over is a person's click, not a hot path, but the lock is held across
    those reads.
  - **Upgrade.** Migration 32 adds `tasks.owner` and `tasks.delegation_state`, returns every
    task already `delegated` to a claim by its assignee in batches of 5,000, and builds
    `ix_tasks_owner` concurrently; it waits two seconds for the table or a delegated row and
    stops, to be started again. During a rolling upgrade a pod still on the old release lets a
    delegate complete or hand on a pending delegation and makes delegations with no owner, so
    finish the rollout before relying on delegation; a rollback after delegations exist returns
    them to the old one-way behaviour. Both are read from the previous release's code, not
    shown by running two versions together. A delegation whose owner's account has gone is handed
    back to that name, and an administrator then assigns or releases it, with a reason.
    `docs/upgrading.md`, *Handing a task over is checked and recorded*.
  - **What a client meets that it did not** (`CHANGELOG.md`, `docs/upgrading.md`). A `reason`
    from anyone but the holder; the target checks; partial `PUT`; `TaskDelegated` and
    `TaskResolved`; a 404 for an administrator naming nobody on a missing task. Also: assigning
    or delegating a task to the person who already holds it is a 400 (it answered 200);
    releasing a task that is not claimed is a 400 for its holder or an administrator (it was a
    500); a malformed body is a 400 in a sentence that names none of the server's types; an
    assignment's `TaskClaimed` carries a top-level `assignee`; an edit that changes nothing
    raises no `TaskUpdated`.
  - **After the whole-branch review** (2026-10-03), fixed on the branch:
    - *A completion in flight overrode a hand-over that committed beside it.* `CompleteTask`
      re-read the task without its row lock, so an administrator's reassignment answered 200
      and the previous holder's completion then completed the task and took it back; the same
      window completed a task that had just been delegated. The re-read holds the row
      (`lockedTask`) and every decision made from the row is made from that read —
      `TestAHandOverMadeWhileATaskIsBeingCompletedDoesNotLoseToTheCompletion`,
      `TestATaskBeingDelegatedIsNotCompletedBeforeItIsHandedBack`.
    - *Lock order, and what it rests on.* A completion, the engine and a migration take the
      instance and then task rows; a claim, a hand-over and an edit hold one task row and then
      only read and insert, so nothing waits the other way. That holds because `audit_logs`,
      `notifications` and `tasks` have no foreign key to `process_instances` in the tables
      the migration runner builds. The storm model's own DDL declares those keys
      (`fk_audit_logs_instance_id`, `fk_notifications_instance_id`, `fk_tasks_instance_id`):
      adding them to the running schema would make a hand-over's audit insert wait for the
      instance row and turn that one-way wait into a deadlock. Whoever reconciles the two
      schemas has to change the lock order first.
    - *Two steps separation of duties keeps apart could both be completed by one person if the
      completions arrived together.* `CompleteTask` asked the rule once, before it held the
      instance, while the other completion was written and not committed. It asks again after
      the instance and the task row are held; completions of one instance take turns at the
      instance lock and run at read committed, so the one that waited sees the other's task
      completed — `TestOnePersonCompletingTwoStepsKeptApartAtOnceCompletesOnlyOne`. The rule
      is one-way, as it always was: a step is barred to whoever did the steps *it* names, so
      two steps that must exclude each other in either order each name the other.
    - *The board had no place for a delegated task.* It is with the claimed ones, and its card
      says and offers what its row does — `TaskInbox.test.tsx` ("the board, for a delegated
      task").
    - *The service took holdership from the actor's name alone.* A signed-in account naming
      somebody else holds nothing — `TestAnAccountActingInAnothersNameIsNotThatPerson` (impl).
    - *A malformed body's 400 named Go types.* —
      `TestAHandOverBodyOfTheWrongKindIsRefusedWithoutNamingTheServersTypes`.
  - **Not in this slice.** Hand-over over gRPC and Connect — REST only, as before; they carry a
    task's `owner` and `delegation_state` and no reason, so there only the holder releases.
    Notifying a candidate group. Editing a task's candidates. A Delegate button in the inbox.
    Telling the owner when a migration retargets a delegated task (a migration that skips or
    cancels one does tell them).
  - **Found, not changed:**
    - The reply to a hand-over takes a different time for "no such account" than for "an
      account in another organization" (one read against four). One repository method that
      finds a member by username and organization closes it and removes a wasted read.
    - Notifications and the stored audit sentence are written by the server in English; only
      the inbox's own words and the timeline's hand-over sentences go through the catalogues,
      and the timeline's other labels ("Step:", "Assignee:") are still English only.
    - A failed notification insert has no savepoint, so it fails the surrounding change rather
      than being skipped, as the log line says it is.
    - `GET /api/v1/tasks/assignee/{assignee}` lists any member's tasks to any member of the
      organization.
    - The inbox's board card offers Complete to an administrator who does not hold the task,
      and the server's refusal there shows a task id; closed tasks on the board still offer
      Edit and Reassign; the inbox reads the signed-in roles from sign-in while the server uses
      the roles in the acting organization; `TaskInbox.tsx` is far past the component-size
      guideline.
    - The migration runner treats its lock as stale after 15 minutes, which a longer concurrent
      index build could outlast (migrations 20, 29 and 32 build one).

- 2026-09-28 (completed, narrowed 2026-10-02): a multi-instance approval finishes cleanly (P0
  reliability, slice 1 of the approval-adjustments work). Branch `mi-approvals-finish`. Driver
  `bpm` · Challengers `go`, `perf`, `test`.
  - **Problem.** An approval several people give, completed from the inbox, left a token on
    the step for each of them, so the process moved on and never completed; and a step ended
    early by its completion condition left the other approvals open, which could move the
    process on twice.
  - **Scope (product owner, 2026-10-02): approvals only.** Counting runs strictly — one
    completion retires one run's token, a completion with no run to retire is refused, a step
    that ends early withdraws what it left open — applies to a repeating **user task or manual
    task** (`entities.Node.IsRepeatingApproval`) and to nothing else. It was first applied to
    every repeating step, and stopped sub-processes that run once per item from finishing: their
    runs share the tokens of the steps inside them, so work owed an advance was refused. Two
    rounds of shape-by-shape fixes each produced new regressions, so every other repeating step
    — sub-process, external task, call activity, service task, script — and every step that
    runs once was put back to what it did before the slice (`90e1413`), looseness included.
    `tests/bpmn/repeating_shapes_unchanged_test.go` drives each of those shapes and compares
    the instance after every step with pins recorded by running the same file against
    `90e1413`.
  - **Follow-up, named:** give each run of a repeating sub-process its own tokens (a design
    task), then make the counting strict for every repeating step and withdraw what an early
    end or an interrupting boundary event leaves in flight.
  - **Acceptance criteria**, each executable by a non-author and each covered by a test:
    1. A parallel approval over three people, each completed through the task service, moves
       the process on once; the instance completes; no token stays on the step
       (`TestAParallelApprovalCompletedThroughTheInboxFinishesItsInstance`).
    2. The same one after another: one task open at a time
       (`TestASequentialApprovalAsksOnePersonAtATimeAndFinishes`).
    3. Two of three: after the second approval the process moves on once, the third task is
       withdrawn and its holder told, and completing it is refused in plain words
       (`TestTwoOfThreeApprovalsEndTheStepAndWithdrawTheThird`).
    4. The same process imported from BPMN XML behaves the same, and export writes the
       condition back (`TestAnImportedTwoOfThreeApprovalBehavesAsADesignedOne`).
    5. A task records its iteration (`tasks.iteration_id`, nullable, migration 31). **The rule
       for a task created before it:** completing it retires the lowest-numbered iteration
       still waiting on its step — numeric order, exactly one, and for a sequential step the
       only one — and when the step finishes every token still on it is removed, so a token
       an earlier completion left behind cannot keep the instance open or move it on twice
       (`TestATaskFromBeforeMigration31RetiresTheLowestIterationStillWaiting`,
       `TestAnApprovalHalfDoneBeforeTheUpgradeStillAdvancesOnce`). The rule is
       `entities.ProcessInstance.WaitingIteration`.
    6. Completing a task that is not open is refused with 400 before the engine is asked
       (`TestCompletingATaskThatIsNotOpenIsRefusedBeforeTheEngine`).
    7. An ad-hoc sub-process whose condition is met withdraws the steps still running inside
       it, at any depth; `cancelRemainingInstances="false"` makes it wait for them instead.
       Absent means true, BPMN's default (`tests/bpmn/adhoc_withdrawal_test.go`).
    8. No engine bookkeeping in the business variables; `go test -race` clean, except
       `tests/handlers` `TestTimerEvent`, a timing-sensitive test this change does not touch.
    9. Every repeating step that is not an approval, and every step that runs once, does what
       it did at `90e1413` (`tests/bpmn/repeating_shapes_unchanged_test.go`, 370 pinned
       steps over 28 tests, designed and imported definitions).
  - **Also closed for approvals, same root:** a completion condition replaced "everyone has
    answered" rather than adding to it, so a threshold the list could not reach held the step
    for ever; an iteration reported twice was counted twice; a deadline on a repeating
    approval fired after everybody had answered, and when it did end the approval it left the
    count of iterations behind. An approval that ended early *before* the upgrade — count
    dropped, a token left for every approver, the other approvals still open — refuses those
    approvals when somebody completes one, and a deadline still queued for it does not fire;
    an approval a deadline interrupted before the upgrade — count left, tokens gone — is
    still moved on by its deadline when the process comes back to it
    (`tests/bpmn/multi_instance_approval_upgrade_test.go`). An approval inside a sub-process
    that runs one item at a time finishes
    (`TestARepeatingApprovalInsideASubProcessRunOneItemAtATimeFinishes`).
  - **Closed with the ad-hoc withdrawal:**
    - An ad-hoc sub-process that finishes ends every step inside it at any depth, stops
      waiting for their boundary events, and withdraws their user and external tasks — the
      external tasks in the same transaction, with one `parked_work_withdrawn` audit entry
      per step. A worker still holding one gets "no such external task". This is the only
      place parked work is withdrawn.
    - External-task `Complete` and `HandleFailure` take the instance lock before the task
      row, the order the withdrawal takes them in. With the old order a report arriving as
      an ad-hoc sub-process finished deadlocked with it (reproduced), and a failure report
      was answered "no such row"
      (`tests/postgres/adhoc_withdrawal_concurrency_test.go`). What a worker without the
      lock is answered is unchanged.
    - A service-task job for a step withdrawn with its ad-hoc sub-process is completed
      without calling its connector; if the step is withdrawn during the call and the call
      fails, no retry and no incident. Only there: a job whose step is not inside an ad-hoc
      sub-process, or whose ad-hoc sub-process is still open, does what it always did.
    - Completing a withdrawn task is `400` (it was `403`), as is completing one already done.
  - **Closed on import, for approvals only:** an imported completion condition on a user
    task or a manual task is the one the engine evaluates, has the marking other modelers
    put around an expression taken off — `${…}`, `#{…}`, a leading `=` — and one the
    evaluator still cannot read is refused on import, naming the step, instead of running
    the step for everybody (`TestAnImportedConditionInAnotherModelersFormStillEndsTheStepAtTwo`,
    `TestAnImportedConditionTheEngineCannotReadIsRefusedAndNothingIsDeployed`). On every
    other repeating step import does what it did before the slice: the condition is kept as
    written in the `multi_instance_completion_condition` property, is not evaluated, is not
    a reason to refuse the file, and is written back on export
    (`TestAnImportedCompletionConditionIsEvaluatedOnlyOnAnApproval`, and the four
    `TestAnImportedRepeating…IsUnchanged` tests of the suite). Made live there it met the
    loose counting those steps keep: one advance per report, or a step that never ends.
    Sequence-flow conditions are imported as written, as before.
  - **Deliberately not done:** instances this defect had already stranded are not repaired —
    `docs/upgrading.md` has the query that finds them. A version imported before this release
    keeps its completion condition where nothing evaluates it and goes on running all-of-N
    until it is imported again — and, for a step that is not an approval, after that too:
    an imported completion condition takes effect on approvals only in this release.
  - **Found and not fixed — for the backlog:**
    - `P0-REL` — **the looseness of every repeating step that is not an approval**, pinned as
      it is by `tests/bpmn/repeating_shapes_unchanged_test.go` and waiting on the follow-up
      above:
      - a repeating external task or call activity keeps its tokens when it finishes, so its
        instance moves on and never completes;
      - a completion condition met early does not withdraw the other runs: their external
        tasks stay on offer and a late report is accepted, their called processes run on and
        their return is accepted, their queued service calls are made — and a condition
        written on business variables can then move the process on a second time;
      - an interrupting boundary event on a step — repeating or not — takes its tokens and
        its user tasks and leaves its external tasks, queued calls and called processes; a
        late report or return then moves the process on from the interrupted step as well as
        down the boundary path, and a failed late call is retried to an incident. A
        repeating step also keeps its count of iterations;
      - a parallel repeating sub-process with a service call inside makes every call, counts
        one run and waits for ever; one with three or more waiting steps inside whose runs
        overlap counts one run too few and waits for ever (the runs share the tokens of the
        steps inside: a run is counted when an end event inside is reached with no other
        token inside);
      - a boundary timer on a repeating sub-process never fires, because the sub-process
        holds no token while its runs are inside it;
      - a repeating script whose completion condition is met part-way still runs for every
        item when it is parallel;
      - a completion condition replaces "every iteration has finished" rather than adding to
        it.
    - `P0-REL` — a repeating approval inside a *parallel* repeating sub-process is asked for
      the first item only, and the sub-process then waits for ever (it waited for ever before
      the slice too, with the approval's tokens left on it). Seen in a probe; needs a test
      with the follow-up.
    - `P0-REL` — an imported completion condition on an approval written against Camunda 8's
      counter names (`numberOfInstances`, `numberOfCompletedInstances`, …) is accepted and
      never holds, and so is one that uses a single `=` to compare with something that is not
      a plain value (`nrOfCompletedInstances = nrOfInstances - 1`): the approval then runs
      all-of-N. `logic.CheckCondition` accepts the one-`=` shape without parsing its
      right-hand side. Either alias or refuse the counter names, and parse the plain shape
      with FEEL too.
    - `P0-REL` — an approval a boundary event interrupted before migration 31 keeps its
      count; when the process comes back to it the step asks nobody until that boundary
      event fires again (as before the slice). With a deadline it mends itself after one
      more deadline; with a message or signal that never comes it stays there.
      `docs/upgrading.md` has the query. Re-entering a step whose count has no run behind
      it should start it afresh.
    - A completion the engine refuses is announced first: `CompleteTask` writes the task and
      dispatches its completed event before the engine can decline (a task an approval that
      ended before the upgrade left open). The transaction rolls back; observers have been
      called in it. Ask before announcing.
    - A repeating approval over an empty list that holds two plain tokens: the first
      completion takes both, the second person is refused and their task stays open.
    - A service call queued for a step of an ad-hoc sub-process that finished and was then
      entered again is made: the sub-process holds a token again.
    - `P0-REL` — BPMN import does not read `zeebe:loopCharacteristics`, so a Camunda 8 file's
      multi-instance step runs once whatever its condition says; the leading-`=` support only
      helps a file that also carries `camunda:collection`.
    - `P0-REL` — BPMN import does not set `parent_id` on the steps inside a sub-process, and
      the engine finds a step's sub-process by it: an imported ad-hoc sub-process never
      re-reads its completion condition, and an end event inside an imported embedded
      sub-process is taken for the end of the process. Read from the code; needs a test.
    - `P0-REL` — BPMN import does not read `multiInstanceLoopCharacteristics` on a
      `<subProcess>`: an imported sub-process that repeats runs once. Seen in a parser test.
    - Tokens a release before migration 31 left on an approval that ended early are not
      removed when they are found; the instance is listed by `docs/upgrading.md` once it has
      nothing in flight. An unreadable completion condition on an *ad-hoc* sub-process, or on
      a version that is deployed rather than imported, still reads as "not yet".
    - `P0-REL` — a boundary event on a multi-instance step is armed once for the step and
      once more for every iteration, so a non-interrupting reminder fires n+1 times.
    - `P0-REL` — finishing a step removes every token on it, so an ad-hoc step started twice
      loses both when the first finishes (in the keep-the-steps mode the sub-process can then
      move on with the second one's task still open); and a terminate end event leaves open
      tasks on the other branches, which can still be completed.
    - `P0-REL` — a process called from a step that has ended — by an early end, an
      interrupting boundary event, or its ad-hoc sub-process finishing — keeps running, and
      its tasks stay in inboxes: nothing ends an instance from outside the migration service.
      When it ends it resumes its parent as it always did; for a step inside a finished
      ad-hoc sub-process that follows the step's outgoing flows, if it has any.
    - `P0-REL` — `EndEventHandler.resumeParent` reads the parent without a row lock, so two
      children of a parallel call activity returning at the same moment can lose a count or
      advance the parent twice. Pre-existing; needs its own fix and a concurrency test.
    - A worker's report on an external task it does not hold the lock for, or whose lock has
      run out, is an unclassified error (5xx over REST) that names the task id; it should be
      a 400 in plain words.
    - An open incident on an external task that was withdrawn with its ad-hoc sub-process
      stays open. Timer jobs of the withdrawn inner steps of an ad-hoc sub-process are not
      deleted; they complete quietly when they fire. A job reclaimed until its attempts run
      out raises an incident without checking that its step is still there.
    - ~~Skipping a repeating approval in a migration counts one iteration and withdraws every
      task, leaving the other iterations' tokens; the in-place waive of slice 3a should end
      the whole step (`Engine.endActivity`).~~ *Done 2026-10-04: see that date's "waive,
      cancel or hold one instance in place" entry (`Engine.FinishActivity`, which a
      migration's skip of a user or manual task calls too).*
    - `tests/handlers` `TestTimerEvent` is timing-sensitive under `-race` on a loaded machine
      (107ms against a 100ms limit, seen once on the untouched baseline).

- 2026-09-28 (completed): storm 0.15.0 → 1.1.0 (Dependabot #134), store regenerated (P0).
  storm v0.16.0 fixed a `MaskCache` that published a column mask and its compiled UPDATE as
  two atomics: two goroutines warming different masks could pair one's mask with the other's
  statement, so an update wrote the wrong columns or failed to bind — invisible to `-race`.
  The generated caches take the new `runtime.MaskKey`, so #134's go.mod bump alone did not
  compile; `make generate` regenerated all 42 files (41 tables) with no hand-written change.
  v1.0.0 made v0.16.0's surface the promised one; v1.1.0 changes nothing Metis calls.
- 2026-09-28 (completed): a confirmed RabbitMQ message survives a broker restart (P0).
  Branch `rabbitmq-persistent-messages`. All three publishes — the bridge's tasks, the
  consumer's dead letters, the RabbitMQ Publisher step — go through `confirmingPublisher`, and
  none set a delivery mode, which RabbitMQ reads as transient. The publisher now sets
  `amqp.Persistent` unless the caller chose a mode. Tests first:
  `TestAMessageIsPublishedToSurviveTheBrokerRestarting` (modes `[0 1]`, want `[2 1]`) and
  `TestABridgesTasksSurviveTheBrokerRestarting` (mode 0, want 2). `docs/integration.md` says
  the bridge's queue has to be durable for it to matter.
- 2026-09-28 (completed): a secret named with a prefix is redacted (P0). Branch
  `redaction-prefixed-secret-names`. The redactor matched a credential's name as the bare word
  between word boundaries, and an underscore is a word character, so `client_secret`,
  `id_token`, `db_password` and `clientSecret` printed their values in logs and error text.
  The name now takes whatever precedes the word in it (`client_`, `spring.datasource.`,
  `X-Auth-`, camelCase), and the compound names (`secret_key`, `access_key`, `private_key`,
  `signing_key`) and `passphrase` are words of their own; nothing may follow the word, so
  `token_type`, `tokenizer` and `password_policy` are left alone. Test first:
  `TestASecretsNameWithMoreToItIsStillASecretsName` (seven of nine cases failed before).
  `configsecret`, which masks connection settings for the browser, already matched by
  substring and needed nothing.
- 2026-09-27 (completed): §9.7 item 7, organization-scoped access — roles are granted per
  organization. Branch `organization-roles`, one commit per change, each with a test that
  fails without it. Driver: sec · Challengers: arch, fe, test.
  - **The gap.** Roles lived on the account, so every role acted in every organization the
    account belonged to, and an organization's administrator granting one (the Users page,
    the matrix, `PUT /api/v1/users/{id}`) granted it installation-wide.
  - **Schema.** Migration 30: `user_organizations.roles`, jsonb, never NULL, default `[]`;
    nothing moved. It waits at most 2s for the table, as 28 does for `audit_logs`, because
    sign-ins read it.
  - **Resolution.** `ProtectedChainWithRoles` resolves the tenant before the role check, and
    the check counts the account's global roles plus the ones it holds in the resolved
    organization (`entities.User.RolesIn`). What no organization owns counts global roles
    only: `PlatformChain` and a new `globalAdmin` chain (`CreateOrganization`, the platform
    accounts). The in-endpoint checks (task hand-over, unnamed work, lookups) read the same
    roles. Roles come from the account on each request, never the token; a change forgets
    the cached account, so a revocation acts at the next request (other replicas within
    `METIS_AUTH_CACHE_TTL`, 5s).
  - **Granting.** `PUT /api/v1/users/{id}/organization-roles` (adminOnly): the tenant's
    organization only, members only (404), built-in roles only (400). Global roles — on
    update, create and delete — take a platform administrator (`internal/pkg/platformadmins`,
    shared with the connector gate). Last-administrator guards: per organization, counting
    ADMIN held there either way; and the last administrator of every organization, which
    no per-organization role could restore.
  - **UI.** The matrix and the account dialog edit the current organization's roles; a
    global role reads *Every organization*, fixed for anybody the platform gate refuses;
    the legend marks global-only actions. English and Indonesian.
  - **Not done:** group-scoped roles (stored, grant nothing). The UI names no organization
    per request (`X-Organization-ID`), so a member of several works in the one the server
    resolves first, as before; pages other than Platform access still decide what to show
    from the sign-in's global roles — never more than the server allows.

- 2026-09-27 (completed): the plan's Phases 2–4 merged as one chain, and 0.4.0 prepared.
  #121, #123, #124, #125, #126 and #127 each contained the PR before it, so CI ran on all of
  them at once and they merged in order without a conflict; main's tree is #127's head.
  - **A refusal over Connect carries its code (#127, P0).** The Connect handlers handed on
    the endpoint chain's refusals as they were, and Connect encodes an error it did not
    make as `unknown`, an HTTP 500: a member calling an administrator's method was answered
    as a server fault. An interceptor on every Connect service gives such an error the code
    REST answers it with (`common.CodeFrom`), redacted as REST redacts it. Test first:
    `tests/connectcodes` failed with `code unknown (…), want permission_denied`.
  - **Migration 28 waits at most two seconds for `audit_logs` (#126, P0).** A canary runs the
    release's migrations beside the stable pods, and an `ALTER TABLE` waiting behind a long
    read queues every audit write behind it. `SET LOCAL lock_timeout`; the upgrade stops with
    the reason and finishes on the next start. Test first: the migration was still waiting
    after 20s with a reader holding the table.
  - **0.4.0 prepared (branch `release-0.4.0-prep`).** `CHANGELOG.md` `[Unreleased]` reads as
    one release: an Upgrading section first — the decisions an operator makes and migrations
    21–29, the two that can stop an upgrade and the one that deletes rows — then one section
    per kind (the two Security sections merged, no entry changed). `docs/releasing.md` is the
    order from `main` to a tag and the 0.4.0 staging checklist; `docs/security-review.md` is
    the scope for an external review, with what is known and open. Tagging `v0.4.0` is the
    product owner's call.
  - **Found, not changed** (listed in `docs/security-review.md`): ~~completing a task accepts
    any variables~~ *(done 2026-09-28: see that date's entry)*; manual tasks are anybody's; a
    service's own refusal over Connect is an HTTP 200 with the reason in the reply; RabbitMQ
    publishes are transient; an external task's lock cannot be extended; redaction misses
    `client_secret`, `id_token`, `db_password`; organization-wide queries carry every project
    id.

- 2026-09-28 (completed): completing a task sets only the variables its form declares — the
  first of `docs/security-review.md`'s known and open findings, P0. Branch
  `task-variables-declared`, one commit per change, each with a test that fails without it.
  Driver: bpm · Challengers: sec, ux, test.
  - **The gap.** `CompleteTask` copied every variable a completion carried into the instance,
    so whoever completed an approval could rewrite its amount, or its approver: business data
    beyond their step. Nothing said which variables a task may set, and no rule read as "any"
    — the `sec` veto on a gap that opens on an absent constraint (AGENTS §2.3).
  - **The rule.** A completion sets only what its task's form declares: the id of each field
    of the task's `form_definition`, hidden ones included because the inbox submits every
    field, and of the stored form its form key names (`forms.fields`, `{"fields": […]}`). No
    form, no variables; a completion with none still completes. Anything else is a 400 that
    names what was refused — sorted, at most ten names of at most 64 characters — decided
    after the task is re-read and re-authorised under the instance's lock and before its
    status is written, so nothing changes. REST, Connect and gRPC share the endpoint and meet
    the same check. External tasks are a different surface, and unchanged.
  - **`METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true`** brings the old rule back for a migration
    window, read and announced at every boot as `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS` is. While
    it is on, the log names each step — project, definition key, node — that sets a variable
    its form does not declare, once, with the variables' names; what has been named is a
    bounded LRU of 1,000 steps. `docs/upgrading.md` has how to find the steps, a query for the
    tasks already waiting, and when to turn it off.
  - **Around it.** The tests whose steps set variables they never declared give those steps a
    form (`testutils.FormDeclaring`), and so does `docs/examples/expense-approval`, which
    `docs/data-flow.md` calls runnable. The inbox needed nothing: it sends exactly the fields
    of the form it shows. The SDK sandbox completes external tasks only.
  - **Found, not changed:** an in-flight migration rebuilds a task from its new node only when
    the node's id changes, so an open task keeps its old form — and what it may set — when a
    step's form changes under the same id. The Go SDK's `examples/quickstart` lives in its own
    repository and was not checked for a completion its step's form does not declare.
  - **Found, not changed** (listed in `docs/security-review.md`): completing a task accepts
    any variables; manual tasks are anybody's; a service's own refusal over Connect is an HTTP
    200 with the reason in the reply; RabbitMQ publishes are transient; an external task's
    lock cannot be extended; redaction misses `client_secret`, `id_token`, `db_password`;
    organization-wide queries carry every project id.
- 2026-09-28 (completed): extend a lock — an external task's lock can be extended, closing
  `docs/security-review.md`'s known-and-open finding and the RabbitMQ entry's backlog item.
  Branch `external-task-extend-lock`, one commit per change, each with its test: the API's
  fail without the change, the bridge's pins a message that already carried enough.
  Driver: arch · Challengers: sec, perf, po.
  - **The gap.** A worker could fetch, complete and fail. Work that outlasted its lock was
    offered to the next worker while the first was still doing it, and ran twice; a bridged
    worker's whole budget was the bridge's `lock_seconds`.
  - **API.** `POST /api/v1/external-tasks/{id}/extend-lock` takes `worker_id` and
    `lock_duration_ms` and answers `lock_expiration`; `ExtendExternalTaskLock` on
    `ExternalTaskService` does the same over Connect and gRPC, a refusal in the reply's
    `error` with no expiry beside it. Wired like `CompleteExternal`: `protected`, scoped to
    the tenant. It gates on no role, so the role legend and its catalogues have nothing to
    list, as for `CompleteExternal`.
  - **Semantics.** Now plus the duration, from 1 ms to a day (`entities.MaxExternalTaskLock`,
    now also the ceiling of a bridge's `lock_seconds`). One conditional UPDATE — id,
    worker_id, lock_expiration > now and the caller's projects — so a fetch for another
    worker, earlier or racing, is never overwritten; only a refusal reads again, to tell 404
    from 400. Another worker's lock, or one that ran out, is a 400 saying to fetch again, as
    a claim on a claimed task is: the engine does not answer 409.
  - **AMQP.** The bridge's message already carried `id`, `worker_id` (`messaging-bridge`)
    and `lock_expiration`, which is what a worker extends with. Every bridge lock has that
    one worker id, so an extension after `lock_expiration` can land on a new delivery's
    lock; the docs say to extend before it. The live-broker test extends in CI.
  - **Found, not changed:** fetch-and-lock over the API bounds `lock_duration_ms` by
    nothing, so a large one overflows the expiry; completing reads the lock, then deletes
    the task, with nothing held between, so a fetch in that gap is undone; `HandleFailure`
    does not check that the lock is live; fetch-and-lock accepts an empty `worker_id`; the
    Go SDK (its own repository) and the SDK sandbox do not extend yet.
  - Verification evidence: `make gate GO_TEST_P="-p 1"` green with `METIS_TEST_POSTGRES_DSN`
    and `STORM_DSN` set against PostgreSQL 17 — 89 packages pass under test, race and the
    strict tenant scope each; UI typecheck, lint (0 errors) and 1592 tests pass.
    `golangci-lint run ./...`: 0 issues. The broker-backed tests skip here, where there is
    no broker, and run in CI; `buf generate` leaves no diff.

- 2026-09-28 (completed): manual tasks are held to HUM-04's rule, as the product owner
  decided the question 0.4.0 left open. Branch `manual-tasks-named`, one commit per change,
  each with a test that fails against the code before it. Driver: bpm (task service, file
  format) and ux (designer, inbox) · Challengers: sec, fe, test.
  - **The file format.** A BPMN file lost a step's candidate users, on every kind of task:
    the parser had attributes for the assignee and the candidate groups only.
    `camunda:candidateUsers` is read and written now, on a manual task as on a user task
    (BPMN 2.0.2 §10.3 gives every activity its performers). Test first:
    `TestWhoDoesAStepSurvivesTheRoundTrip`, and `tests/bpmn`
    `TestAManualTaskFromAFileIsOfferedToThePeopleItNames` ("offered to [] and [warehouse]").
  - **The designer.** A manual step's panel had a line of free text the engine never read,
    and said an empty one was anybody's. It has the user step's *Who does this* now — one
    shared `AssignmentSettings` — and keeps the free text as a note. The warning for a step
    that names nobody covers manual steps; an assignment table does not satisfy it there,
    because the engine asks a table only for a user step.
  - **The rule.** `entities.Task.FallsToOperators` exempted `ManualTask` and
    `authorizeCandidate` let a candidate-less manual task through. Claiming, completing,
    delegating and assigning a manual task that names nobody take an administrator or an
    operator, with the user task's 403 for anybody else; `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS`
    covers both kinds. The board's mirror (`fallsToOperators`) follows. Test first:
    `tests/task` over both kinds ("claiming a manualTask nobody was named for: got 200").
  - **Upgrading.** Every manual step designed before names nobody, so its tasks go to the
    administrators and operators. `docs/upgrading.md`'s query no longer filters manual tasks
    out; the CHANGELOG says what to do. `docs/security-review.md` finding 2 is closed.
  - **Found, not changed:** *All Tasks* (`TaskList.tsx`) offers Complete on every open task
    to whoever reads it, of either kind, and the server refuses the ones it must. *Assign to*
    shows nothing for an assignee who is not one of the organization's users — a name from
    a BPMN file, say — though the engine carries it, as it did for user steps. An assignment
    table cannot route a manual step.
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
    Creating, re-enabling or re-pointing one still needed one; fixed on
    `environments-live` (entry below).
  - #95: a live-update hint is sent once the work it points at has committed.
  - #96: access control (organization-scoped group membership, 403 for a missing role,
    self-service profile, case-insensitive roles).
  - #97–#102: UX items 5, 8, 9 (in part), 11, 10 and 12, as §9.7 records.
  - #103: load/chaos testing and the thousand-row reads (entry above).
  - The last PR: the architecture audit and its eight cheap fixes.
  - Decisions left open are in the final report and in the Serena memory
    `verified-findings`: manifest scoping and the built-in override, the canary cohort,
    OIDC users' organizations, the RabbitMQ bridge, decision versioning (since decided and
    done, entry below), whether a service task's script runs, and task-edit authorization.
- 2026-09-26 (completed): decision versions (DMN-06), as the product owner decided it.
  Branch `decision-versions`; each change with a test that fails without it.
  - **Saving a decision keeps the version it replaced.** A save stores the edit as the
    key's next version and never changes a stored one (the repository has no Update); a
    save that changes nothing stores nothing.
  - **Which version is live is recorded** on `decision_releases`, a release timeline with
    the process timeline's shape and read rule. An evaluation that names no version (a
    business rule task with no version binding, the evaluate API, a required decision)
    reads the live version; a key with none refuses rather than falling back to the
    newest. A save can be staged; any version can be made live again, including an older
    one. Scheduling is not offered.
  - **Migration 26** records, per project and key, the version that evaluated before the
    upgrade (the highest one not deleted) as live.
  - **The decision list is one row per decision**, with its live and newest versions and
    when either last changed; the editor asks whether a save goes live and has a version
    history (open, make live, roll back, delete a version).
  - **Delete** removes one version, and refuses the live version while others remain.
    ~~OIDC users' organizations~~ (decided and delivered as `IAM-05`, entry below), the
    RabbitMQ bridge, decision versioning, whether a service task's script runs, and
    task-edit authorization.
- 2026-09-26 (completed): `IAM-05` — somebody signing in through OIDC can be placed in an
  organization. Branch `oidc-organizations`.
  - The gap: with `OIDC_ISSUER` and `OIDC_CLIENT_ID` set, every OIDC user was refused with
    401 on every organization-scoped endpoint. The token's claims were the principal and
    carry no membership, so the tenant resolver — rightly, since P0-SEC-05 — refused them.
    OIDC was unusable in practice.
  - Decided by the product owner and delivered: `METIS_OIDC_ORGANIZATION_CLAIM` names the
    claim listing a person's organizations, matched by organization **id** (names are not
    unique and change); a first sign-in creates an account linked to (issuer, subject),
    never by email, with no role — the inbox needs none; the account is the principal,
    admitted to exactly the organizations its token's claim names, and each sign-in makes
    its memberships match the claim (every membership a linked account has came from its
    claim, so nothing an administrator granted is undone). No claim configured, none in
    the token, or none naming an organization here: 403 naming what is missing, and no
    account. Migration 27 adds `users.identity_issuer`/`identity_subject`.
  - Tests that failed first: `tests/auth/oidc_organizations_test.go` and
    `oidc_refusals_test.go` against a fake issuer (go-oidc's `oidctest`) — 401 before;
    `tests/migrations/user_identity_link_test.go` — the index missing before.
  - Found, not changed: while OIDC is on, the API takes only the provider's tokens and
    refuses a local account's with 401 — the mandatory interceptor has one strategy. It
    was so before and is documented now; running both is a decision of its own.
    **Decided 2026-09-26: both kinds, each by its own rules** (entry below, branch
    `auth-both-tokens`).
  - Verification evidence: `make gate` green with `METIS_TEST_POSTGRES_DSN` and `STORM_DSN`
    set against PostgreSQL 17 — 83 packages pass under test, race and the strict tenant
    scope each; UI typecheck, lint (0 errors) and 1377 tests pass.
    OIDC users' organizations, the RabbitMQ bridge, decision versioning, whether a
    service task's script runs, and task-edit authorization. (The RabbitMQ bridge was
    decided and done 2026-09-26, INT-15: entry below.)
- 2026-09-26 (completed): Executed INT-15 — the RabbitMQ bridge and inbound consumer run
  when configured. Branch `rabbitmq-bridge`. The product owner chose to wire them up behind
  configuration, off by default, rather than remove them and their docs.
  - **The gap.** The README advertised inbound correlation and an external-task bridge;
    `StartBridge`, `StartInboundConsumer` and `StopAll` had no callers, so a running server
    held no broker connection at all (architecture audit, "Found on the way").
  - **Configuration.** `METIS_RABBITMQ_BRIDGES` (project, connection, topic, exchange,
    routing key) and `METIS_RABBITMQ_CONSUMERS` (project, connection, queue, message), each
    a JSON list, in the operator's environment and not the API. The broker is reached
    through a RabbitMQ connection of the project on the Connectors page, so its password
    stays where it is encrypted, masked and rotated. An entry that cannot be read is
    named and skipped; a project or connection that does not exist is retried from 5s,
    doubling to 5 minutes; neither stops the server.
  - **Scope.** Each runs under the project's organization as its tenant, not as system
    work, which would have forwarded every organization's tasks of a topic to one
    project's broker. Stopped with `StopAll` beside the job worker's drain.
  - **Logs.** Every line a bridge or consumer writes names it, and each says when it
    connects and reconnects.
  - Tests: `internal/app/rabbitmq_*_test.go` and
    `server/domains/services/impl/messaging_identity_test.go`, each failing before its
    change. The broker-backed ones skip without `METIS_TEST_RABBITMQ_URL` and run in CI.
  - **Found and not fixed — for the backlog:**
    - A bridge's lock is a fixed 30 seconds, and it is the downstream worker's whole
      budget, time on the queue included. A task not completed in time is published
      again at the next poll, so a backlog on the queue multiplies itself. Wants a
      per-bridge lock setting; changing `StartBridge` for it needs a broker to prove.
      **Fixed on `rabbitmq-hardening` (entry below):** `lock_seconds`, five minutes
      unless set.
    - A channel the broker closes — an exchange that does not exist, or one the user may
      not publish to — is not reopened while the connection lives, so the bridge hands
      every task back at every poll until it is restarted. `runBridge` should reopen a
      closed channel as it redials a closed connection. Needs a broker to prove.
      **Fixed on `rabbitmq-hardening` (entry below).**
    - A bridge's publish waits for its confirm with no deadline of its own, so a blocked
      broker holds the fetched tasks past their lock. **Fixed on `rabbitmq-hardening`
      (entry below):** `METIS_RABBITMQ_CONFIRM_TIMEOUT`, 10 seconds unless set.
    - A bridge takes its topic from every project of the organization, as an API worker
      does; there is no per-project fetch.
    - Reconnection after start is every 5 seconds, not backed off. **Fixed on
      `rabbitmq-hardening` (entry below).**
    service task's script runs, and task-edit authorization.
- 2026-09-26 (completed): the RabbitMQ bridge and consumer survive a broker that
  misbehaves — four of the five items INT-15 left for the backlog. Branch
  `rabbitmq-hardening`, one commit per gap, each with a test that fails against the code
  before it. Driver: arch · Challengers: perf, sec, test.
  - **A confirm that never comes.** Every publish — the bridge's, the consumer's
    dead-letter publish, the RabbitMQ connector's — waits at most
    `METIS_RABBITMQ_CONFIRM_TIMEOUT` (10s) for its confirm. It waited for ever: the
    bridge held the task and its topic until a restart, and the connector a job worker's
    slot. A missed confirm is a refusal: the task is handed back, the rest of the round
    unpublished, and the channel replaced, because the broker's late answer about the lost
    message could be read as its answer about the next.
  - **What the broker closes is opened again.** The bridge and the consumer watch
    `NotifyClose` on their channel and connection and reopen the channel alone while the
    connection is up. The consumer says why its deliveries stopped (the broker's reason,
    or that it cancelled the consumer) and declares the queue again. Each problem is logged
    at error once, naming the bridge or consumer, and at debug while it lasts
    (`problem_log.go`, bounded).
  - **Backoff.** Reconnecting waits 5s doubling to 5 minutes, ±25%, and starts over once
    a connection has lasted: to the bridge's next round or a forwarded task, through
    the consumer's first 5 seconds of consuming. A broker that drops each connection
    straight away is waited for as if it were down. It reuses the schedule `backoff.go`
    had for job retries, now a `backoff` type, rather than a second one. (The
    broker-backed backoff test first failed in CI: it changed the proxy while the bridge
    ran, and raced the dial it meant to follow. It now changes it only while the bridge
    is held between rounds.)
  - **A lock long enough for a queue.** `lock_seconds` per entry of
    `METIS_RABBITMQ_BRIDGES`, 30 to 86400, 300 unless set. A worker cannot extend it, so it
    covers queue time and work; `docs/integration.md` says how to size it. Tasks fetched
    as the bridge stops are handed back at once instead of waiting out the lock.
  - **How it is tested without a broker.** The loops depend on narrow, consumer-owned
    interfaces (`brokerConnection`, `brokerChannel` and the ones they compose), which
    `amqp_connection.go` and `amqp_channel.go` adapt the library to, and run in the tests
    against a broker in memory. What only a real broker can vouch for runs in CI:
    `messaging_broker_test.go` (a missed confirm, through a proxy that drops
    `basic.ack`/`basic.nack` frames; a missing exchange created mid-run; a deleted queue;
    backoff and its reset through a proxy that refuses and cuts) and the lock in
    `internal/app/rabbitmq_broker_test.go`. The proxy's framing is tested locally.
  - Verification evidence: `make gate` green with `METIS_TEST_POSTGRES_DSN` and
    `STORM_DSN` set against PostgreSQL 17 — 83 packages pass under test, race and the
    strict tenant scope each; UI typecheck, lint (0 errors) and 1469 tests pass.
    `golangci-lint run ./...`: 0 issues. The broker-backed tests skip here, where there is
    no broker, and run in CI.
  - **Found and not fixed — for the backlog:**
    - *Done 2026-09-28 over HTTP, gRPC and Connect, not yet in the SDK: see that date's
      "extend a lock" entry.* A worker cannot extend an external task's lock: the API
      has fetch-and-lock,
      complete and failure, on every transport. A bridged worker's whole budget is
      therefore the bridge's lock, so a long job needs a long lock, and a lost message
      waits that long to be published again. An extend-lock operation would let locks
      stay short; it is API surface on HTTP, gRPC and Connect, and in the SDK.
    - The bridge publishes transient messages, so a broker restart loses what is queued
      and those tasks wait out their lock. Persistent delivery is a small change and a
      decision about the broker's disk.
    - Still nothing measures a bridge's or consumer's connection, so still no alert and
      no runbook.
    - A bridge takes its topic from every project of the organization (above).
- 2026-09-26 (completed): environments go live without a restart, and each one's backlog
  is measured. Branch `environments-live`; the two items left open by #94 and by the
  observability batch.
  - **Served without a restart (E6).** Creating an environment, enabling one again or
    giving one another port or database now takes effect on every replica within a
    check (15 s): boot is the first pass of the same check, so there is one code path
    for opening, migrating under the schema lock, binding and working an environment.
    A change is detected by the port and a keyed digest of the connection, never the
    password itself. One that cannot start is retried at every check, does not hold up
    the others, and is logged once per cause. Test: `internal/app/environment_start_test.go`.
  - **Measured per environment (E5).** The backlog gauges carry `environment` (the id)
    and `environment_name`; the main database's series are unchanged. An unreadable
    environment is `metis_engine_state_up 0` on its own; the databases are read at once
    inside the two-second scrape budget. The engine alerts name the environment. Test:
    `internal/app/engine_metrics_test.go`, promtool cases in `alerts_test.yaml`.
  - **Found and fixed on the same branch:** an environment's database got the GORM
    migrations but not storm's tables or column defaults (`db.EnsureTables`,
    `db.EnsureColumnDefaults` ran on the main database only), so a storm insert there — a
    job, for one — failed with `storm: not-null constraint violated`: no timer, service
    task or retry could run in any environment. `openEnvironmentStorm`, the shared open
    path, now runs the same step (`ensureStormTables`) before registering the pool. Test:
    `TestAnEnvironmentsDatabaseTakesTheJobsItsProcessesSchedule`.
- 2026-09-26 (completed): A captured webhook delivery can no longer be replayed (the
  backlog item from the engine-reliability entry). Branch `webhook-replay`, one commit per
  change, each with a test that fails against the code before it:
  - v2 signatures: `X-Metis-Signature: v2=` HMAC-SHA256 of `<timestamp>.<delivery id>.<body>`
    with `X-Metis-Timestamp` and `X-Delivery-Id`; five minutes either way; explanations only
    once the signature has matched, so they cannot find addresses.
  - Body-only (v1) signatures bounded per webhook by `legacy_signatures_until`: migration 25
    gives existing webhooks 90 days, new webhooks get none, and a v1 delivery after the date
    is refused with how to move.
  - The webhooks screen shows each webhook's date and the v2 help; `docs/integration.md` has
    the scheme with Go, Node.js and Python examples checked against a live server.
  - Not done: a way to close a webhook's window early from the API or screen (SQL for now,
    in `docs/upgrading.md`).
- 2026-09-26 (completed): audit order — the two audit items the thousand-row entry left
  open. Branch `audit-order`, one commit per change, each with a test that fails against
  the code before it:
  - **An instance's trail reads in the order it was written.** The entries one transaction
    writes share its created_at and their ids are random, so the timeline, the execution
    path and the OCEL export got them in storage order, which is the write order only until
    a row moves. Measured on a 200,000-entry table: rewriting one entry per transaction in
    place, as the reseal after a key rotation does, reversed 748 of the 1,600
    same-transaction neighbours in one instance's trail. Migration 28 adds `audit_logs.seq`,
    numbered by an owned sequence as each entry is written, and the trail is read by
    (created_at, seq). A sequence rather than UUIDv7: the database assigns it whoever
    writes (the audit observer leaves the id to the column's default, and an old release
    keeps writing during a rolling upgrade), replicas need not agree about the time, and
    trails already recorded keep their order where sorting by random ids would shuffle
    them. Entries written before the migration are not numbered and keep the order they
    had. The business timeline shows the trail reversed instead of re-sorting it by a
    timestamp one step's entries share. Tests: `tests/bpmn/audit_write_order_test.go` (the
    table clustered on its primary key; before, the path began at the task),
    `tests/migrations/audit_write_order_test.go`, `BusinessTimeline.test.tsx`.
  - **Each case in the OCEL export names the process version it ran.** The instance row
    carries only its definition's id, and the export used it as it came: every case said
    `definition_key` "" and `definition_version` "0" and was related to no definition, so a
    case of v3 and one of v4 were the same to a miner. The export now reads each version
    once, through the engine's tenant-keyed definition cache, and fills in the key, the
    version and the relation to the `key:version` definition object it always declared. A
    case whose definition was deleted carries none of them rather than version 0. Tests:
    `tests/bpmn/ocel_version_test.go` (an instance of v2 and one of v3; before, both said
    version "0") and `TestOCELNamesNoVersionItCouldNotRead`.
  - **Found, not changed:** a migrated case carries the version it runs now, stamped at
    its first event. OCEL can carry the change as a second time-stamped value at the
    migration; the `instance_migrated` entry already records the source and target
    versions it would take.
- 2026-09-26 (completed): `DMN-17` — a decision cell sees the rest of the case. Branch
  `decision-cells-see-inputs`, one commit per change, each with a test that fails without it.
  - **The gap.** A condition cell was tested against its own column's value and nothing
    else (`ruleMatches` built `{"_input": value}`), so `> minimum` beside a minimum column,
    `> credit_limit`, or `[low..high]` compared with null: no match and no error, `!=`
    matching every case, and only a range failing ("cannot compare a number with a null").
  - **The rule now.** DMN's: the column's value is the implicit subject (`_input`, bound
    first, so no variable shadows it) and every variable the decision was evaluated with is
    in scope by name, required decisions' answers included — the names the columns read,
    so a column and a variable of the same name are one value, and a heading is not a name.
    The bare-word deviation stays, narrowed: a lone word is text unless it names one of the
    table's columns, so a table's words do not change meaning with the process that consults
    it; `= name` reads any variable. `?` and names with spaces stay outside the subset
    (execution-plan §2.1, pinned by `TestSubsetIsDocumented`). Outputs are literals and
    unchanged. `ExpressionEvaluator.EvaluateBool` became `MatchesCell` with an explicit
    `entities.DecisionCellScope`.
  - **Cost.** A variable is converted when a cell reads it: a cell beside a 5,000-line
    order went from 1.6 ms and 10,005 allocations to 159 ns and none; a 100-line table from
    38.7 µs and 339 allocations to 28.1 µs and 2.
  - **Editor.** The checks no longer read a lone column name as a word (that produced a
    false overlap error that blocked Save); hover text names the column; the coverage card
    says what a column is compared with; a line under the grid says a condition can name
    another; `?` is marked with what to write instead. Found on the way and fixed in their
    own commits: the card told an author to quote any unread column that had a `-` line,
    and a broken condition's `aria-invalid` never reached the page.
  - **Upgrading.** No migration. Tables whose cells name a variable decide as written from
    the upgrade on; `docs/upgrading.md` has a query that lists those cells, and rolling the
    release back restores the old reading.
  - Tests: `tests/decision/conformance_test.go` (new corpus cases),
    `tests/decision/cell_scope_test.go` (a business rule task on PostgreSQL),
    `impl/decision_cell_scope_test.go` (precedence; the caller's variables untouched),
    `feel/cell_variables_test.go` (allocation bound), and the UI's `columnComparison`,
    `columnComparisonHint`, `ConditionCell` and editor page tests.
  - Verification evidence: `make gate` green with `METIS_TEST_POSTGRES_DSN` and `STORM_DSN`
    set against PostgreSQL 17 — 83 packages pass under test, race and the strict tenant
    scope each; UI typecheck, lint (0 errors) and 1485 tests pass. The changed packages run
    verbose: 506 pass, none fail, and the only two skips need a RabbitMQ broker.
    `golangci-lint run ./...`: 0 issues.
  - **Not done, and why:** `?` is flagged, not implemented (it means DMN 1.2's boolean unary
    tests). A range whose end names a variable nobody supplied still fails the evaluation,
    as a range with an incomparable bound always has; changing it would change gateway
    conditions too. Try it gives values to the table's columns only, so it cannot supply a
    variable no column reads; the coverage card says so rather than suggesting Try it. The
    Serena `verified-findings` memory and the PRD row for DMN-17 live outside the
    repository and are updated once this merges.
- 2026-09-26 (completed): Signing in stays possible when the identity provider is not, as
  the product owner decided it. Branch `auth-both-tokens`, one commit per change, each with
  a test that fails against the code before it.
  - **Local accounts sign in while OIDC is on.** The API took the provider's ID tokens
    alone, so turning OIDC on cost the break-glass administrator. `TokenKindStrategy`
    (`server/interceptors/auth`) accepts both, and chooses whose rules check a token by
    what it says it is, not by trying one and falling back: an HMAC naming no issuer is
    local (checked against `JWT_SECRET`, unchanged), a public-key signature is the
    provider's (go-oidc, then the account link and the organization claim, unchanged),
    anything else is refused unchecked. A fallback would make the looser rules decide what
    the stricter refused — a `JWT_SECRET` token naming the provider as issuer would pass the
    local check, which never reads an issuer. Reading the kind costs ~575 ns and 4
    allocations per request, only with OIDC on (an HS256 parse alone is ~1.75 µs and 45).
    Test: `tests/auth/both_tokens_test.go`, both kinds with OIDC on and off — the local
    token was 401 with OIDC on; replacing the dispatch with a provider-then-local fallback
    makes the forged-issuer row 200 and the test fail. Unit tests for the dispatch rule in
    `server/interceptors/auth/token_kind*_test.go`.
  - **`--reset-password` refuses an account linked to an identity provider**, naming the
    provider to reset it at. It set a password on one, which then signed in without the
    provider — a way in the provider could not revoke. The refusal is in `SetPassword`,
    the command's only caller; nothing about the account changes. Test:
    `internal/app/reset_password_test.go` — before, "Password updated for "ada"" and the
    password signed in; after, refused and the hash still empty. A local account still
    resets.
  - **The redactor keeps the words of an error.** Its colon rule redacted whatever
    followed a secret's name, so the first word of every wrapped auth error was lost
    (`missing or invalid token: ***REDACTED*** ID token names…`) — and, spending its
    match on that word, left the value after it alone: `token: jwt: <a token>` kept the
    token. The value is kept now only when it reads as prose — a space after the colon, a
    plain word, more words after it on the same line — and matching resumes at a kept word.
    `token=`, JSON, URL queries and `Authorization` headers are unchanged. Test: the
    table in `internal/pkg/redaction/redactor_test.go`, both directions and the existing
    cases — 14 rows failed before (13 eaten words, 1 token left in clear).
  - **Found, not changed:** a secret's name with a prefix joined by `_` is not recognised by
    any rule — `client_secret`, `id_token`, `db_password` pass through in JSON, queries
    and key/value text, before and after this branch, because `\b` does not match after
    `_`. A connector error carrying `?client_secret=` would be stored in an incident in
    clear. Allowing `(?:[a-z0-9]+[_-])*` before the names is the likely fix; it widens
    what every rule redacts, so it wants its own change and test.
- 2026-09-26 (completed): `HUM-04` — a task nobody was named for is no longer anybody's,
  as the product owner decided it: deny by default, administrators and operators may take
  one. Branch `unassigned-task-claims`, one commit per change, each with a test that fails
  against the code before it:
  - **Claiming and completing** a user task with no assignee and no candidates takes an
    administrator or an operator; anybody else gets a 403 that names who can.
    `authorizeCandidate` had read "no candidates" as "anyone" — the `sec` veto on an
    authorization gap that opens on an empty field (AGENTS §2.3).
  - **Handing one on** (delegate, assign) follows the same rule, so an operator can give it
    to the person it should have gone to. Releasing and editing are unchanged.
  - **The inbox** lists such tasks under *Available to Claim* for administrators and
    operators, and the board no longer offers a member Claim on one.
  - **The designer** warns (not an error) about a user task with no assignee, no candidates
    and no assignment table; the property panel's suggestion says the same, and no longer
    misreads a step offered to a team. Deploy has no warnings channel, so the server's
    deploy is unchanged.
  - **`METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true`** brings the old rule back for a migration
    window, off by default and announced at boot when on; `docs/upgrading.md` has the
    query for the tasks affected.
  - **Open, for the product owner:** ~~manual tasks. The designer has no field to name
    anybody for one and tells its author an empty one is anybody's, so they were left
    open; closing them needs that field first. ~~And a completion can still carry variables
    open; closing them needs that field first.~~ *Done 2026-09-28: see that date's
    "manual tasks" entry.* And a completion can still carry variables
    of the completer's choosing on a task they may take — a manual task's included, which
    asks nobody for any.~~ *Done 2026-09-28: a completion sets only what its task's form
    declares; see that date's entry.*
- 2026-09-26 (completed): what tenant scoping costs an organization with ten thousand
  projects, measured. Branch `tenant-scope-at-scale`. Every scoped repository call reads the
  organization's project ids and filters on `project_id = ANY(ids)`; since the scope reads
  every project (5d5016b), past a thousand, nobody had measured it past a handful.
  - **Measured (P1).** `tests/loadtest` `TestTenantScopeAtScale`: 10,000 projects against 4,
    the same 20,000 instances and tasks in each. The list was read once per scoped call —
    six times a statistics request, five an instance list — at about 4ms and 10 MB a read:
    statistics p95 26.3ms against 2.1ms, 201ms on a loaded run, 55 MB allocated a request.
    Tasks by assignee paid 31.7ms against 2.1ms with one read, because a generic plan walks
    the 10,000-element array for every row. `metis_tenant_scope_reads_total` counts the
    reads. Numbers in `docs/performance.md`.
  - **Read once per request (P1).** `tenantscope.Request`: the tenant resolver gives each
    request a place to keep its organization's project ids, and every scoped call in it
    reuses the first read. Ended with the endpoint, forgotten when the request creates or
    deletes a project, never used for a context naming another organization, so nothing is
    cached across requests. Test first: `tests/slo` `TestEachRequestReadsItsTenantScopeOnce`
    failed with 5 reads to start a process, 5 for the instance list, 6 for the statistics and
    9 to complete a task; each is 1 now. Isolation through the kept scope, under the default
    and the strict scope, in `tests/tenant/request_scope_test.go`. Large-organization p95
    after: statistics 6.3–9.8ms (was 26.3–201.3), instance list 6.2–6.6ms (was 35.6–142.0).
    A subquery on `projects.organization_id` instead of the list was not an option: storm has
    no subquery predicate, and an environment's instances and tasks are in a different
    database from its projects.
  - **The ids only (P1).** `projectsOf` read whole project rows through the store, ten keyset
    statements at 10,000 projects; it reads the ids in one statement now, with the store's
    soft-delete predicate written out (pinned by the deleted-project test). Large-organization
    p95 after both: statistics 2.9–6.0ms, instance list 3.6–7.1ms, a task by id 1.8–4.9ms,
    against 1.7–3.8, 2.2–4.2 and 0.31–0.85ms for 4 projects; allocation per request 0.7–2.1 MB
    where it was 9–55 MB.
  - **Still open: the list inside the query.** Reads spanning the whole organization still
    pay for 10,000 ids per statement: the inbox 7–10ms more, tasks by assignee 8–55ms more,
    because on its generic plan PostgreSQL filters the assignee's rows against the array one
    id at a time (21–26ms against 4.9ms planned with the values). A `tasks (assignee,
    project_id)` index was tried; the generic plan does not use it. Everything is inside the
    150ms target at 10,000 projects, but the cost grows with projects × rows. The fixes are an
    organization column on the tables a project owns (schema change and backfill, main and
    environment databases) or planning these statements with their values (a pool-wide
    change to how every query is planned). Neither is done; measure with
    `TestTenantScopeAtScale` before and after.
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
