# Architecture audit: facade, interfaces, patterns

The three open items under `.junie/roadmap.md` §9.2, checked against commit
`ad1b477` on `roadmap-architecture`, 2026-09-25.

**Since then** (#93–#95, `roadmap-chaos` and this branch): defects 1.1, 3.6,
3.7, 3.8, 3.9, 3.12, 3.13, 3.14 and 3.15 are fixed, 1.2 in part, and the
environment defect found on the way; the eight cheap fixes are done; and the
load tests found and fixed reads that stopped at a thousand rows. Each is listed, with the test that
fails without it, under
[Fixed since the data was taken](#fixed-since-the-data-was-taken). The rows
below are left as found, so the reasoning stays readable.

ServiceFacade itself is orchestration-only — the struct behind it declares no
methods and delegates every call — but six endpoint functions hold rules that
belong in a service, and the task rules around them have two gaps that let a
caller reopen finished work or take over another person's task. Interfaces are
small on paper and wide in use: 19 endpoint constructors take the 177-method
facade to call at most 17 of its methods, and 14 services take the 27-accessor
repository to use one to seven. Of the six patterns, Unit of Work carries most
of the defects — two write paths commit half an operation, three
read-modify-write paths take no row lock, and process-event webhooks are still
sent before commit — Strategy carries one, a Null Object that turns an unknown
node type into a silent hang, and Repository, Adapter, Decorator and Observer
have smells only.

---

## How to read this

- The leads came from a static-analysis report generated earlier the same day
  against an older commit (its sections A–R are cited as "report §X"). Every row
  below was re-read in the current tree, and every `path:line` is current. Report
  items that no longer hold are under
  [Fixed since the data was taken](#fixed-since-the-data-was-taken) or left out.
- Judged against `AGENTS.md` §0 and §2 and `.junie/guidelines.md` §2–3.
- Nothing was run: no build, no tests, no gate. Where a row names a test, it is
  the test to write.
- **Defect** can produce wrong behaviour. **Smell** is design debt with no
  behavioural failure. **Size**: S is under half a day, M one to three days, L
  longer.
- `impl/` is `server/domains/services/impl/`, `contracts/` is
  `server/domains/services/contracts/`, `handlers/` is
  `server/domains/handlers/impl/`. Other paths are from the repository root.

---

## 1. Does ServiceFacade only compose and delegate?

**Verdict: the facade does. The layer above it does not.**

`ServiceFacade` (`server/domains/services/facade.go:8-47`) embeds 22 contracts,
177 methods. The struct behind it (`server/domains/services/service.go:13-36`)
embeds the same 22 and declares no methods, so every call passes through
unchanged. No business rule lives in the facade.

The problems are next to it. Its constructor is a second composition root, it
hands engine internals to every endpoint, and six endpoint functions hold rules
a service should own.

| Measure | Count |
| :-- | :-- |
| Methods on `ServiceFacade` | 177, from 22 embedded contracts |
| Methods declared on the struct behind it | 0 |
| Endpoint constructors that take the whole facade | 19: `server/endpoints/endpoints.go:56` and 18 of the 19 domain packages |
| Endpoint packages that take a narrow contract | 1: `setup` (`server/endpoints/setup/endpoint.go:17`) |
| Facade methods the edge or the composition root calls on the facade value, counting where it is held as `SetupService` or `WebhookService` | 137 |
| Facade methods nothing calls on the facade value | 40, 13 of them engine methods |
| Endpoint functions calling two or more facade methods (report §F) | 9: three hold logic (1.4–1.6), six are request dispatch (see Not worth fixing) |
| Endpoint functions holding logic that report §F missed | 3: delegate, assign and the inbox, through helpers (1.1, 1.3) |

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 1.1 | Handing a task over is checked only in the endpoint, and only for who asks. `DelegateTask` and `AssignTask` accept a task in any status. A completed or cancelled task keeps its assignee, so that person can delegate it; the delegate can then complete it, and completion advances the node a second time — its outgoing path runs again. The job path guards against exactly this with a token check; the task path has none. | `server/endpoints/task/endpoint.go:327-343`; `impl/task.go:225-248`, `446-469`, `267-281`, `353`; `impl/engine.go:431-469`; the guard it lacks: `impl/job.go:444-450` | Defect | Move the rule into the service. `DelegateTask` and `AssignTask` take the actor, read the task locked, and refuse unless it is claimed or delegated (or unclaimed, for an administrator assigning) and the actor may hand it on. Before `Proceed` in `CompleteTask`, check the token as `tokenWaitsAt` does. Test: complete a task, delegate it as the completer, complete it as the delegate; the next node runs once. | M |
| 1.2 | Nobody checks who may unclaim or edit a task. Both endpoints run on `protected` alone and the service takes no actor. Any signed-in account in the tenant can unclaim a task another person holds. A task assigned directly and given no candidates is then open — `authorizeCandidate` reads no candidates as "anyone" — so the same account can claim it and complete it with its own variables. | `server/endpoints/endpoints.go:280`, `283`; `server/endpoints/task/endpoint.go:149-162`, `255-274`; `impl/task.go:197-223`, `416-444`, `153-156` | Defect | `UnclaimTask(ctx, id, actor)`: only the holder or an administrator. The same rule, or `operator`, for `UpdateTask`. Test the refusal for a third account. | S |
| 1.3 | The inbox endpoint expands the caller's groups, by name and by id, itself. The service does the same expansion for claim and complete. Two copies of who belongs to what. | `server/endpoints/task/endpoint.go:303-322`; `impl/task.go:168-181` | Smell | One service method that resolves the caller's groups, used by both. | S |
| 1.4 | Connector update folds the stored credentials back over the placeholders in the endpoint: read in one transaction, write in another, so two concurrent edits can lose one. The same mask-and-merge rule lives in the service for environments and in the repository for directory sources. | `server/endpoints/connector/endpoint.go:128-152`; `impl/environment.go:108`, `195`; `server/repositories/pg/participant_source.go:105`, `228` | Smell | Merge inside `UpdateConnectorInstance`, in one unit of work. Keep the rule in the service layer for all three. | S |
| 1.5 | The migration endpoint plans, then applies — and apply plans again. Every applied migration surveys the source version twice, and the plan returned with `Applied: true` is the first one, not the one applied. | `server/endpoints/definition/endpoint.go:342`, `349`; `impl/migration.go:70` | Smell | `MigrateInstances` returns the plan it applied; the endpoint makes one call. | S |
| 1.6 | The instance list is assembled in the endpoint from three reads — page, counts, attention — and the endpoint decides the chip policy: drop the status and attention filters before counting. It parses statuses with the persistence type. | `server/endpoints/process/endpoint.go:68-142` (`110-112`), `16`, `170-171` | Smell | One read method on the engine that returns the page with its counts. | M |
| 1.7 | The facade exposes 13 engine methods that no edge caller uses, among them `ExecuteNode`, `Proceed`, `UpdateInstance`, `GetInstanceForUpdate` and `DispatchEvent`. None does today, but any endpoint could advance an instance without the task service's checks. | `server/domains/services/facade.go:31`; `contracts/engine.go:18-91` | Smell | Embed an edge-facing engine contract in the facade: start, reads, signal, message, script. Keep the rest on `ExecutionEngine` for services and handlers. | M |
| 1.8 | The facade's constructor is a second composition root inside the domain package. It builds some 25 objects, picks the locker, wires the cycles, and imports GORM for the setup callback — so every package that imports `services` for the interface also compiles every implementation and GORM. | `server/domains/services/service.go:3-11`, `91-195` (`107`, `148-155`) | Smell | Move `NewServiceFacade` and the struct into `internal/app` or a wiring package. The interface stays in `services`, as `.junie/guidelines.md` §3 requires. | M |
| 1.9 | The edge reaches into implementation packages. The user endpoint calls `impl.LocalUserIDFromContext`; the webhook transport matches `impl` error values; the HTTP transport takes `*impl.SSEObserver`; four endpoint packages import repository contracts for paging types, and one imports `repositories/models` (report §O). | `server/endpoints/user/endpoint.go:125`; `server/transports/https/webhooks/handler.go:126-127`; `server/transports/https/http.go:45`; `server/endpoints/{decision,definition,process,task}/endpoint.go`; `server/endpoints/process/endpoint.go:16` | Smell | Error values into `contracts`; the context helper into `server/endpoints/principal`; paging types and status enums into `entities`. | S each |

---

## 2. Are interfaces small and consumer-centric?

**Verdict: no.** The big contracts are split into parts, and almost nothing
holds a part. Consumers take the unions.

| Interface | Methods | Held by, in production | Methods each holder uses |
| :-- | :-- | :-- | :-- |
| `ServiceFacade` | 177 | 19 endpoint constructors; `InterceptorFactory`; `NewHTTPHandler`; `App.svc`; `BuildAPIHandler` | 1–17; 1; 2; 7; forwards |
| `repositories.Repository` | 27 accessors | 14 live services, 3 dead ones; `App.repo`; `NewServiceFacade`; 2 backfills | 1–7 (services); 9 (`App.repo`) |
| `ExecutionEngine` | 25 | 7 fields: 6 services and the handler factory | 1–5; the factory forwards 13 |
| `JobConnectorService` | 21 | `jobService.connectorSvc` (`impl/job.go:41`) | 4 |
| `TaskService` | 15 | 2 handler fields | 1: `CreateTaskForNode` |
| `DecisionService` | 9 | 2 handler fields | 1: `Evaluate` |
| `JobService` | 9 | 2 handler fields and `Engine.jobSvc` | 1 each |

Facade methods called per endpoint package, out of 177:

| Package | Used | Package | Used | Package | Used |
| :-- | --: | :-- | --: | :-- | --: |
| `connector` | 17 | `platformuser` | 6 | `notification` | 4 |
| `definition` | 15 | `environment` | 5 | `participantsource` | 4 |
| `process` | 14 | `organization` | 5 | `webhook` | 4 |
| `task` | 12 | `participant` | 5 | `external_task` | 3 |
| `group` | 9 | `project` | 5 | `incident` | 2 |
| `decision` | 8 | `user` | 7 | `collaboration` | 1 |

`setup` takes `contracts.SetupService` and uses all 3 of its methods.

Repository accessors used per live service: 1 (environment, group, user,
webhook), 2 (external task, organization), 3 (connector, definition),
4 (decision, job), 5 (project), 6 (engine, task), 7 (migration).

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 2.1 | Every endpoint package but `setup` takes the whole facade. The largest user, `connector`, calls 17 of 177 methods; `collaboration` calls 1. | `MakeEndpoints` in `server/endpoints/*/endpoint.go`; contrast `server/endpoints/setup/endpoint.go:17` | Smell | Each package's `MakeEndpoints` takes the contracts it calls. `endpoints.MakeEndpoints` keeps the facade and passes it down; it satisfies each. Mechanical, 18 packages. | M |
| 2.2 | The interceptor factory holds the facade to call `ValidateToken`. Importing `services` for it also pulls in every implementation (1.8). | `server/interceptors/factory.go:20`, `23`, `74` | Smell | Cheap fix 1. | S |
| 2.3 | 14 services hold the whole repository and use one to seven accessors. No smaller interface over the accessors exists, so narrowing means passing the per-table repositories and the unit of work. | e.g. `impl/environment.go:22`, `impl/group.go:15`, `impl/user.go:22`, `impl/webhook.go:48` (1 each); `impl/migration.go:21` (7) | Smell | Constructors take the per-table contracts they use. Start with the four single-accessor services. | M |
| 2.4 | Seven fields hold the 25-method engine to call one to five methods. Two map to an existing part: the webhook service calls only `SendMessage`, and `NewAdHocSubProcessHandler` stores an `EngineRunner` but takes the whole engine. The rest span runner and reader, which no exported part covers — though the handlers package already declares consumer-side interfaces that do. | `impl/webhook.go:49`, `53`; `handlers/adhoc_subprocess.go:23`; `impl/external_task.go:18`; `impl/migration.go:31`; `impl/task.go:25`; `impl/job.go:40`; `impl/adhoc_activator.go:34`; the model to copy: `handlers/events.go:23`, `31`, `40`, `handlers/gateways.go:20` | Smell | Cheap fix 2 for the two. Consumer-side interfaces for the rest. | S–M |
| 2.5 | Segregation on paper. Fourteen of the parts the big contracts are split into are never held as a value; they exist only inside the unions: `EngineReader`, `ScriptExecutor`, `ConnectorReader`, `ConnectorWriter`, `ConnectorInstanceManager`, `ConnectorRegistry`, `ConnectorManifestManager`, `ConnectorRequestRunner`, `JobEnqueuer`, `JobWorker`, `IncidentManager`, `ConnectorStepTrier`, `DecisionEvaluator`, `DecisionManager` (report §C). Five fields could hold `JobEnqueuer` or `DecisionEvaluator` today. | `contracts/engine.go:29`, `78`; `contracts/connector.go:11-63`; `contracts/job.go:11-45`; `contracts/decision.go:17-22`; the five fields: `impl/engine.go:36`, `handlers/tasks.go:24`, `53`, `handlers/events.go:187`, `handlers/business_rule.go:26` | Smell | Hold the parts (Cheap fix 2). Fold back any part still unheld after 2.1–2.4. | S |
| 2.6 | Contracts whose only implementation nothing constructs: `FormService`, `DeploymentService`, `HeatmapReader`, `SLAReporter`, `SagaCoordinator` and its two parts, `MessageBrokerAdapter` and its two parts. Two contracts have no implementation at all: `contracts.TenantResolver` and `tenant.TenantResolver`. `entities.Acceptable` is referenced nowhere (report §C, §D). | `contracts/form.go:10`, `contracts/deployment.go:10`, `contracts/heatmap_reader.go:12`, `contracts/sla_reporter.go:12`, `contracts/saga_coordinator.go:12-27`, `contracts/broker_adapter.go:7`, `contracts/tenant_resolver.go:12`; `server/interceptors/tenant/middleware.go:18`; `server/domains/entities/visitor.go:11` | Smell | Delete with their implementations: Cheap fixes 5 and 6. The heatmap pair is a decision (Not worth fixing). | S |
| 2.7 | Methods on live contracts that nothing calls, tests included (report §G): `GetDefinitionByKey`; `ListTasksByAssignee` and `ListTasksByCandidates`, whose comment claims internal callers; four `UserService` assign and unassign methods; `GetRootInstance`; `GetPlatformUser`; `EnqueueBoundaryTimer`; `AdHocActivator.IsComplete`; `participantsource.Source.Kind`; `TaskRepository.ListWithFilters`; `Repository.CompensatableActivity`. Twelve more are called only by tests. | `contracts/definition.go:67`; `contracts/task.go:21`, `28` (comment `23-26`); `contracts/user.go:36-39`; `contracts/engine.go:58`; `contracts/platform_user.go:18`; `contracts/job.go:20`; `contracts/adhoc_activator.go:20`; `impl/participantsource/source.go:37`; `server/repositories/contracts/task.go:21`; `server/repositories/facade.go:50` | Smell | Delete the ones with no caller: Cheap fix 8. Leave the test-only ones until 2.1 settles what the edge needs. | S |
| 2.8 | Contracts past the guideline's five methods that are not composed of parts. Service: `DefinitionService` 16, `TaskService` 15, `UserService` 14, `EngineReader` 12, `GroupService` 9, `DecisionManager` 8, `EngineRunner` and `PlatformUserService` 7, and five at 6. Repository: `DefinitionRepository` 19, `ProcessRepository` and `TaskRepository` 16, `UserRepository` 14, `DecisionRepository` 10, and thirteen more at 6 to 9. | `contracts/*.go`; `server/repositories/contracts/*.go` | Smell | Split along the consumer lines 2.1 and 2.3 reveal, not ahead of them. | L |
| 2.9 | Service contracts carry persistence types. The engine reader takes repository filter and page types and returns counts keyed by `models.ProcessStatus`; `TaskFilter` uses `models.TaskStatus`. This is how `models` reaches the endpoints (1.9). | `contracts/engine.go:42`, `47`, `55`; `server/repositories/contracts/task.go:10-15` | Smell | Status enums and paging types in `entities`. | M |

Worth copying:

- The handlers declare consumer-side interfaces next to their use:
  `eventBusRunner`, `endEventEngine`, `terminateEventEngine`
  (`handlers/events.go:23`, `31`, `40`) and `parallelJoinEngine`
  (`handlers/gateways.go:20`).
- The webhook transport takes `contracts.WebhookService`
  (`server/transports/https/webhooks/handler.go:51`); the setup endpoints take
  `contracts.SetupService`.
- The engine holds only `VariableHistoryWriter` (`impl/engine.go:37`), the
  notification service only `NotificationRepository` (`impl/notification.go:14`),
  the messaging service only `EngineEventBus` (`impl/messaging.go:60`).
- Optional capabilities are found by type assertion instead of widening a
  contract: `RequestExecutor`, `SharedLimiter`, `responseStarted`.

---

## 3. How are the patterns used?

| Pattern | Where | Verdict |
| :-- | :-- | :-- |
| Repository | One contract per table (`server/repositories/contracts/`), one PostgreSQL implementation each (`server/repositories/pg/`), composed at `server/repositories/repository.go:50` | Used. Two writers outside it, and a legacy model layer in its contracts. |
| Unit of Work | `contracts.UnitOfWork` (`server/repositories/contracts/uow.go:6-20`): `Do`, `Attempt`, `AfterCommit` over `db.Conn.Transact` (`server/repositories/db/db.go:218`) | The machinery is sound. The defects are at seven call paths (3.6–3.9, 3.12–3.14). |
| Strategy | Node handlers, connector executors, `DistributedLocker`, `SecurityStrategy`, `IdempotencyStore`, SQL dialects, directory sources, `DeliveryScheduler` | Used. One silent Null Object default, one drifted duplicate. |
| Adapter | `server/domains/adapters` (model to entity), `server/transports/adapters` (entity to protobuf), connectors and directory sources (external systems) | Used. One extra hop, and the caller's identity has no adapter. |
| Decorator | Transport and endpoint interceptors; metrics, recovery and tracing wrappers; `deliveringNotificationService` | Used at the edge only. One redundant layer, three never wired. |
| Observer | One dispatcher with audit, SSE, webhook and notification observers (`internal/app/app.go:650-681`) | Used. Two observers act before commit; they are counted under Unit of Work (3.6, 3.7). |

### Repository

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.1 | Two model layers under the contracts. Repositories read storm rows and hand out GORM-tagged `models` structs, which services then convert to entities. `models` carries 158 GORM tags, still defines the migration schema, and is imported by 38 files above the repository layer. | `server/repositories/model/doc.go:3-6`; `server/repositories/models/*.go`; e.g. `server/repositories/contracts/environment.go:16` | Smell | Contracts return entities. `models` shrinks to the migration schema, then goes. | L |
| 3.2 | Idempotency records are read and written from an interceptor straight through storm, outside the repository layer. | `server/interceptors/security/idempotency_db_store.go:13-40`; `server/interceptors/factory.go:66-71` | Smell | An idempotency repository in `server/repositories/pg` behind a contract. The interceptor keeps its `IdempotencyStore` strategy. | M |
| 3.3 | The setup wizard writes the first organization, project and administrator with GORM models directly, in its own transaction, beside the storm repositories every other writer uses. | `impl/setup.go:445-500` | Smell | Seed through the repositories. | M |
| 3.4 | A write-only global. `SetDBOverride` sets a value only `ResolveDB` reads, and `ResolveDB` has no callers. The setup callback logs a hot swap that no longer happens; nothing re-opens the storm connection after boot. This is the global the `arch` veto names. | `server/repositories/gorms/open.go:81-108`; `internal/app/app.go:668-669`; `internal/app/storm.go:38-41`, `internal/app/app.go:299` | Smell | Delete the global: Cheap fix 4. Then decide whether setup needs a restart, and say so in the log. | S |
| 3.5 | `NewRepository`'s comment describes two connections and a GORM port that no longer exist. | `server/repositories/repository.go:41-49` | Smell | Rewrite the comment. | S |

### Unit of Work

The machinery is right. `Transact` joins an enclosing transaction instead of
nesting (`server/repositories/db/db.go:218-261`). `Attempt` takes a savepoint
and rewinds the commit hooks registered inside it
(`server/repositories/db/db.go:279-327`). Commit hooks run only after a commit
(`server/repositories/db/db.go:252-259`,
`server/repositories/db/after_commit.go:75-84`). Task completion, the job
paths, ad-hoc activation and migration apply lock the instance before changing
it (`impl/task.go:303`, `impl/job.go:440`, `impl/job.go:935`,
`impl/adhoc_activator.go:61`, `impl/migration.go:365`, `1197`).

**Network calls reachable inside a transaction.**

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.6 | Process-event webhooks are sent from inside the transaction. `WebhookObserver.OnEvent` runs during `Dispatch`, which the engine calls inside its unit of work, and starts one goroutine per endpoint at once. The POST races the commit and is made even when the transaction rolls back, so a partner can be told about a process that never started. It also uses its own `http.Client` instead of the egress-guarded shared one, and the goroutines are unbounded. Report §J skipped `go` statements, which is why it did not list this. | `server/domains/observers/impl/webhook.go:26-31`, `33-53` (`51`); registered at `internal/app/app.go:655-660`; dispatched inside a transaction at `impl/engine.go:167`, `366` | Defect | Send after commit on a bounded queue, as notifications now do (`impl/notification_dispatch.go:31-48`), through `httpclient.Shared()`. Test: start a process whose first step fails; the receiver sees nothing. | S |
| 3.7 | Live-update hints reach browsers before the data they point to commits. The SSE observer writes to local browsers during `Dispatch` and queues the cross-replica copy for a separate connection. A browser that refetches on the hint can read the old state, and gets no second hint. | `server/domains/observers/impl/sse.go:78-109`; `server/domains/observers/impl/sse_fanout.go:159-175`; `server/repositories/pg/broadcast.go:31-32` | Defect (low: a stale view until the next event) | Schedule the broadcast with `AfterCommit`, computing the scope first. | S |

Checked and outside any transaction: service-task calls (`impl/job.go:425`,
before the unit of work at `439`), "Try it" (`impl/job_try.go:40`), directory
sync fetches (no unit of work in `impl/participant_sync.go`), and the connection
tests in setup and environments. The script sandbox and the node handlers make
no network calls.

**Writes that should commit together.**

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.8 | Webhook receipt commits the delivery claim, then sends the message in a separate transaction. If sending fails — a lock timeout, a failed advance — the partner retries, the retry finds the claim, and it is answered as a duplicate. The event is never acted on (report §K). | `impl/webhook.go:97`, `122`; `server/repositories/pg/webhook.go:142-158` | Defect | Claim and send in one unit of work. A concurrent duplicate then waits on the unique index and sees the outcome. Test: fail the first send, retry the same delivery id, expect the message delivered. | S |
| 3.9 | A migration skip runs three transactions: cancel the step's tasks, read the instance without a lock, advance it. If the advance fails, the tasks stay cancelled and the token stays — nobody can finish the step, and no incident says so. A change to the instance between the read and the advance is overwritten. | `impl/migration.go:1155`, `1159`, `1170`; called outside any transaction from `impl/migration.go:336` | Defect | One unit of work: lock the instance, cancel the tasks, advance, record the decision. Test: skip a node whose advance fails; the task is still open. | S |
| 3.10 | Migration audit entries are written after the commit, on purpose: the comment says a failed audit write should not undo a migration. The guidelines ask for the audit write inside the transaction. A migration resumes where it stopped, so writing inside costs a re-run on failure, not a stranded instance (report §K3). | `impl/migration.go:441`, `454-460`, `1173`, `1237`, `1503` | Smell (a documented trade-off) | Write inside the unit of work. | S |
| 3.11 | Delete guards check and delete in separate statements. A definition version or a decision found unused can gain a user before the delete lands. | `impl/definition.go:325-350`; `impl/decision.go:185-206` | Smell (a narrow race) | Check and delete in one unit of work with a lock, or a conditional delete. | S |

**Read-modify-write without a row lock.**

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.12 | External-task completion changes the instance after an unlocked read and writes all of it back; the row update has no version check. Two external tasks on parallel branches that complete together lose one advance — the race the service-task path was fixed for (report §E). | `impl/external_task.go:66`, `80`, `90`; `server/repositories/pg/process.go:81-108`; the fixed twin: `impl/job.go:430-440` | Defect | `GetInstanceForUpdate`. Test shaped like `tests/postgres/multi_instance_concurrency_test.go`. | S |
| 3.13 | Task transitions read the task without a lock and write every column back: claim, unclaim, delegate, assign, update. Two people claiming at once both succeed; the later write wins and the other is told they hold a task they do not. `TaskRepository` has no locking read (report §I). | `impl/task.go:91-128`, `197-248`, `416-469`; `server/repositories/contracts/task.go:18-39`; `server/repositories/pg/task.go:267-305` | Defect | Add `TaskRepository.GetForUpdate`, as `ProcessRepository` has (`server/repositories/pg/process.go:73`), and use it in all five; or update on the expected status. Test: two concurrent claims, exactly one succeeds. | S |
| 3.14 | External-task failure is read, checked and written with no transaction, so a worker whose lease expired can clear the lease of the worker that took the task over. Found on the way: at zero retries it logs `// Log incident?` and raises none, and `retryTimeout` is stored but never read, although the HTTP API documents it as the wait before the task is offered again — so a failing task is offered again at once, indefinitely (report §K). | `impl/external_task.go:94-120` (`112`); `server/repositories/pg/external_task.go:147-152`; `server/transports/https/external_tasks/handler.go:115-117` | Defect | A unit of work with a locking read; an incident at zero retries; the lease set to now plus `retryTimeout`. | S |

### Strategy

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.15 | An unknown node type is a silent hang. The handler factory's default returns a Null Object that logs and returns nil, so the token stays where it is with no incident. Deploy validation refuses only an empty type, so any other string in a definition reaches it. `AGENTS.md` §0: no swallowed errors on the execution path (report §L). | `handlers/factory.go:101-102`; `handlers/null.go:14-21`; `server/domains/validation/visitor.go:37-44` | Defect | Refuse, at deploy, any type the factory does not handle. Make the default an error so the instance raises an incident. Test: deploy a definition with a `sendTask` node; expect a refusal. | S |
| 3.16 | The factory builds two handler objects for every node it executes, though the handlers hold only dependencies. | `handlers/factory.go:52-105`; called per node at `impl/engine.go:375` | Smell (an allocation per step on the engine's hot path) | Build the handlers once in the constructor, keyed by type. | S |
| 3.17 | Two outbound-auth strategies have drifted. The HTTP task runner reads credentials from the node — the model a designer edits and exports — and sends the call unauthenticated when one is missing. The manifest runner reads them from the connection's configuration and refuses when one is missing; its own comment says credentials must never come from the model. The default API-key header differs: `X-API-Key` against `Authorization`. | `impl/http_task_runner.go:124-138`; `impl/connectors/manifest_runner.go:190-240` | Smell (a `sec` concern) | One auth applier for both runners, reading only connection configuration. | M |
| 3.18 | Directory sync documents a lock that stops a scheduled run and a manual one reading the same directory at once. It is wired with `NoOpLocker`, which always grants. | `impl/participant_sync.go:78-91`; `internal/app/app.go:667` | Smell | An in-process lock, since one replica is the supported topology, or `PostgresLocker`. | S |
| 3.19 | The participant import endpoint repeats the source kinds as string literals in its own switch. | `server/endpoints/participant/endpoint.go:61-72`; `impl/participantsource/source.go:18-27` | Smell | One service method taking a `participantsource.Kind`. | S |

Used as intended: node handlers behind `NodeHandler` (`handlers/factory.go:52`);
connector executors registered by key (`impl/connector.go:466`); JWT or OIDC
behind `SecurityStrategy` (`server/interceptors/auth/strategy.go:10`);
`IdempotencyStore` in memory or in the database; SQL dialects
(`impl/sqlconnector/dialect.go:10`); directory sources; `DeliveryScheduler`.
`NoOpLocker` for jobs is a choice with its reason written down
(`server/domains/services/service.go:148-155`). DMN hit policies are one switch
over the closed set the spec defines, with no second copy; a strategy would add
nothing.

### Adapter

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.20 | Two conversions on every read: storm row to `models`, then `models` to entity (3.1). | `server/repositories/pg/*.go`; `server/domains/adapters/*.go` | Smell | Goes with 3.1. | L |
| 3.21 | The caller's identity is decoded from the context in ten places across five packages, each with its own type switch; some accept four principal types, some two. A new principal type has to be added to every one. | `server/interceptors/tenant/resolver.go:118`; `server/interceptors/security/idempotency_interceptor.go:194`; `server/interceptors/auth/rbac.go:97`; `server/interceptors/auth/interceptor.go:22`; `impl/user_identity.go:21`, `39`, `63`; `server/endpoints/principal/principal.go:26`, `62`, `85` | Smell | One principal adapter in `internal/pkg/auth` that returns a normalised principal. | M |
| 3.22 | Adapters used only by dead services: compensatable activity, deployment, form (report §H). | `server/domains/adapters/compensatable_activity.go`, `server/domains/adapters/deployment.go`, `server/domains/adapters/form.go` | Smell | Delete with the services: Cheap fix 5. | S |

Used as intended: `server/domains/adapters` maps models to entities with object
references, as `.junie/guidelines.md` §3 requires; `server/transports/adapters`
maps entities to protobuf; connectors and directory sources adapt external
systems.

### Decorator

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.23 | Every API request is authenticated twice. The HTTP handler wraps an optional JWT layer inside the mandatory layer `BuildAPIHandler` adds. With JWT the token is parsed twice; with OIDC the inner layer fails and passes the request through. | `server/transports/https/http.go:49-50`, `102`; `internal/app/app.go:868`, `886` | Smell | Drop the inner layer. `tests/user/http_user_test.go:78` builds the handler without `BuildAPIHandler` and would need the outer one. | S |
| 3.24 | Three decorators written and never wired: `InterceptorChain`, the tenant HTTP interceptor and the endpoint tenant guard. The wired resolver already refuses a caller with no membership. `AGENTS.md` §2 (`arch`): dead architecture (report §H). | `server/interceptors/chain.go`; `server/interceptors/tenant/middleware.go:14-63`; wired instead: `server/interceptors/tenant/resolver.go:58-94` | Smell | Delete: Cheap fix 6. | S |

Used as intended: `deliveringNotificationService` adds delivery around the
notification service (`impl/notification_delivery.go:37-93`); transport
interceptors and endpoint interceptors are composed in
`internal/app/app.go:880-900` and `server/interceptors/factory.go:93-120`;
metrics, recovery and tracing wrap the handler (`internal/app/app.go:925-933`).
No service or repository has a decorator. Tracing and the circuit breaker sit
inline in the job and connector services, the two places that need them; a
decorator for two callers would be ceremony today.

### Observer

| # | Finding | Where | Class | Proposed fix | Size |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 3.25 | `OnEvent` returns nothing, so an observer cannot fail the operation. When the audit observer's insert fails it logs; on PostgreSQL the transaction is then aborted, and the operation fails later with "current transaction is aborted" instead of the audit error. | `server/domains/observers/contracts/process.go:10-12`; `server/domains/observers/impl/audit.go:131-137` | Smell | Let in-transaction observers return an error, or write the entry in a savepoint with `Attempt`. | M |
| 3.26 | Two narrative tables for the same event types. Task events now carry the writer's text (`event.Audited`), so the observer's task cases run only when no writer is wired. | `server/domains/observers/impl/audit.go:38`; `impl/audit_writer.go:81` | Smell | One narrative function. | S |
| 3.27 | `NullProcessObserver` is never used (report §H). | `server/domains/observers/impl/process.go:10-13` | Smell | Delete: Cheap fix 7. | S |

Used as intended: one dispatcher (`server/domains/observers/impl/process.go:15-33`)
with its observers registered at the composition root
(`internal/app/app.go:650-681`). Notifications are stored in the transaction and
delivered after it.

---

## Fixed since the data was taken

| Report item | What changed | Where |
| :-- | :-- | :-- |
| §J: all 34 paths from a transaction to `http.Client.Do` or `smtp.SendMail`, every one through notification delivery | Delivery is scheduled with `AfterCommit` onto a bounded queue; storing the notification stays in the transaction | `impl/notification_delivery.go:87-93`; `impl/notification_dispatch.go:31-48`; `server/repositories/db/after_commit.go:75-84`; `server/repositories/db/db.go:252-259`; `internal/app/app.go:1341-1343` |
| §K, §K2: `runJob` wrote the job status outside the work's transaction | Completion is written in the work's transaction | `impl/job.go:449`, `465`, `920`, `929`, `953`, `965`; `impl/job_settlement.go:50-55` |
| §K: `createIncident` and `rescheduleRepeatingTimer` outside a transaction | The incident is written with the failed job; the reschedule runs inside the timer's unit of work | `impl/job_settlement.go:121-143`; `impl/job.go:926`, `962` |
| §K3: `adHocActivator.ActivateTask` updated the instance outside a transaction | Runs in a unit of work on a locked row | `impl/adhoc_activator.go:54-61` |
| Task audit entries written twice, by the writer and by the observer | The raiser marks the event audited; the observer skips it | `impl/task.go:477-481`; `server/domains/observers/impl/audit.go:26-28` |
| §B: `UnitOfWork` with two methods; GORM repositories escaping transactions | Three methods (`Do`, `Attempt`, `AfterCommit`); every repository on storm, one transaction | `server/repositories/contracts/uow.go:6-20`; `server/repositories/uow.go:10-16` |
| §G: `WebhookService.ForgetOldDeliveries` never called | Called by the retention sweep, as a method value | `internal/app/retention.go:71` |
| 1.1: a completed or cancelled task could be delegated or assigned, and so completed again | Hand-overs hold the task row and refuse one that is completed or withdrawn | `impl/task.go` `openTaskForHandOver`; `tests/task/reopen_test.go` |
| 1.2, in part: anyone in the tenant could release a task another person held | Only the holder or an administrator can. Editing a task is still unchecked | `server/endpoints/task/endpoint.go` (Unclaim → `mayHandOver`); `tests/task/release_test.go` |
| 3.6: process-event webhooks were sent from inside the transaction | Sent after commit, through `UnitOfWork().AfterCommit`. The observer still has its own `http.Client`; its endpoints come from `WEBHOOK_ENDPOINTS`, the operator's, not a user's | `server/domains/observers/impl/webhook.go`; `server/domains/observers/impl/webhook_after_commit_test.go` |
| 3.8: webhook receipt committed the claim, then sent the message separately | One unit of work; the claim is an insert that does nothing on conflict, so a PostgreSQL transaction is not aborted by it | `impl/webhook.go` `Receive`; `server/repositories/pg/webhook.go` `ClaimDelivery`; `tests/webhook/retry_after_failure_test.go` |
| 3.9: a failed migration skip left the step's task cancelled and its token in place | One unit of work, the instance locked before the task, as `CompleteTask` does | `impl/migration.go` `skipNode`; `tests/instancemigration/skip_failure_test.go` |
| 3.12: external-task completion lost one of two parallel advances | Completion reads the instance for update | `impl/external_task.go` `Complete`; `tests/bpmn/external_task_parallel_test.go` |
| 3.13: claim, unclaim, delegate, assign and update read the task without a lock | Every one takes a locking read, `TaskRepository.GetForUpdate`, so none writes a stale copy over a completion | `impl/task.go` `lockedTask`; `tests/task/claim_race_test.go`, `tests/task/release_race_test.go` |
| Found on the way: deleting an environment left it served until a restart | Deleting or disabling one stops its listener, its workers and its connections on every replica within 15 s (#94). Creating one, enabling it again or giving it another port or database starts it on every replica within 15 s, through the same check that boot runs first (`environments-live`) | `internal/app/environment_watch.go`, `environment_runtime.go`; `internal/app/environment_runtime_test.go`, `environment_start_test.go` |
| The eight cheap fixes below | Applied, one commit each, on this branch. Nothing listed was still referenced, except `principal.LocalUserID`, which the self-service profile (#96) now calls and which stays | this branch's history |
| 3.7: live-update hints reached browsers before the data they point to committed | Delivery waits for `UnitOfWork().AfterCommit` and is dropped on a rollback, local and cross-replica alike | `server/domains/observers/impl/sse.go` `DeliverAfterCommitWith`; `tests/sse/after_commit_test.go` (#95) |
| Found by the load tests: reads that stopped at a thousand rows | A generated storm query starts with a limit of 1,000, and reads the engine acts on relied on it as if it were unbounded: a signal woke the first thousand waiting instances, a migration moved the newest thousand, a deadline withdrew a thousand tasks. Those reads now walk every row with a keyset cursor (`pg.everyRow`) | `server/repositories/pg/every_row.go`; `tests/bpmn/signal_audience_test.go` and the tests beside each fix (`roadmap-chaos`) |
| 3.14: external-task failure ran without a transaction, raised no incident at zero retries, and never read `retryTimeout` | One transaction; an incident at zero; the wait is honoured; a sweep offers again a task stranded at zero with no incident | `impl/external_task.go` `HandleFailure`; `server/repositories/pg/external_task.go` `ReofferStranded`; `tests/bpmn/external_task_failure_test.go`, `tests/postgres/external_task_reoffer_test.go` |
| 3.15: an unknown node type was a silent hang | Deploy refuses a type the engine does not declare; the null handler fails | `server/domains/validation/visitor.go`; `handlers/null.go`; `tests/bpmn/unknown_node_type_test.go` |

---

## Found on the way

Outside the three questions, found while checking them.

| Finding | Where | Class | Proposed fix |
| :-- | :-- | :-- | :-- |
| The README advertises RabbitMQ inbound correlation and the external-task bridge. Nothing starts either: `StartBridge`, `StartInboundConsumer` and `StopAll` have no callers, so the 589-line messaging service is built into the facade and unreachable. `docs/recovery.md` names `PostgresLocker` as the lock for that bridge. | `README.md:15-16`; `impl/messaging.go:81`, `192`, `534`; `docs/recovery.md:93-97` | Defect (a `pm` veto: a claim the code does not deliver) | Wire them, or take the claims out of the README and the recovery notes. |
| Deleting an environment removes its row and nothing else. The listener and workers started for it at boot run until the next restart, although the service's comment says removing the row stops it being served. `ForgetEnvironmentDB`, written for this, has no production caller (report §H). | `impl/environment.go:115-120`; `server/repositories/gorms/environments.go:53-61`; `internal/app/environment_listeners.go:34`, `123` | Defect | Stop the environment's listener and workers on delete, or say that a restart is needed. |
| `TruncateScriptOutput` is documented as the bound that stops a script exhausting memory, and is never called; no other bound by that name exists. Whether the sandbox's other budgets cover it was not checked (report §H). | `server/domains/logic/sandbox.go:316-326` | Unverified risk | Apply it where script results are read, or delete it and the claim. |

---

## Cheap fixes

**Done** on this branch, one commit each. Still open from 3.4: the setup
callback's documentation (`setup.go`, `OnSetupCompleteFunc`, and its comment in
`internal/app/app.go`) describes a database hot swap that no longer happens.

Each is mechanical and changes no behaviour. After each: `make build vet lint`,
then the tests named. Suites under `tests/` that need PostgreSQL run under
`make test-db`; without a DSN they skip.

1. **Narrow the interceptor factory.** `server/interceptors/factory.go:9`, `20`,
   `23`: `services.ServiceFacade` becomes `contracts.UserService`; it calls only
   `ValidateToken` (`:74`). The three callers pass the facade and compile
   unchanged. Test: `go test ./server/endpoints/ ./server/interceptors/...`.
2. **Narrow six fields and three parameters to the part they call.**
   `impl/engine.go:36`, `49` to `JobEnqueuer`; `impl/webhook.go:49`, `53` to
   `EngineEventBus`; `handlers/tasks.go:53` and `handlers/business_rule.go:26` to
   `DecisionEvaluator`; `handlers/tasks.go:24` and `handlers/events.go:187` to
   `JobEnqueuer`; the parameter at `handlers/adhoc_subprocess.go:23` to
   `EngineRunner`. Every caller already passes a value that satisfies the part.
   Test: `go test ./tests/bpmn/... ./tests/webhook/... ./tests/handlers/...`.
3. **Drop the handler factory's unread `connectorService`.** Field
   `handlers/factory.go:16`, parameter `:36`, assignment `:46`, and the argument
   at its 23 call sites (`server/domains/services/service.go:156`, 22 in
   `tests/`). Test: `make vet`.
4. **Delete the write-only database override.**
   `server/repositories/gorms/open.go:81-108` (`dbOverrideMu`, `dbOverride`,
   `SetDBOverride`, `ResolveDB`) and the call with its log line at
   `internal/app/app.go:668-669`. Nothing reads the value. Test: a grep for the
   four names finds nothing; `go test ./internal/app/...`.
5. **Delete services nothing constructs, tests included,** with the contracts
   and adapters only they use. Form: `impl/form.go`, `contracts/form.go`,
   `server/domains/adapters/form.go`. Deployment: `impl/deployment.go`,
   `contracts/deployment.go`, `server/domains/adapters/deployment.go`. Saga:
   `impl/saga_coordinator.go`, `contracts/saga_coordinator.go`,
   `server/domains/adapters/compensatable_activity.go`. Broker:
   `impl/noop_broker.go`, `contracts/broker_adapter.go`,
   `contracts/broker_publisher.go`, `contracts/broker_subscriber.go`,
   `server/domains/entities/broker_message.go`. Also `impl/null_job.go` and
   `impl/noop_error_boundary_matcher.go`. The repositories and tables stay.
   Test: `make build vet`.
6. **Delete the never-wired interceptors:** `server/interceptors/chain.go`,
   `server/interceptors/tenant/middleware.go`, `contracts/tenant_resolver.go`.
   Test: `go test ./server/interceptors/...`.
7. **Delete unused functions:** `NullProcessObserver`
   (`server/domains/observers/impl/process.go:10-13`), `PassThroughHandler`
   (`handlers/tasks.go:139-146`), `NewCatchableError`
   (`server/domains/entities/catchable_error.go:21`), `HumanizeError`
   (`server/domains/logic/errors.go:9`), `ToUUIDPtr`
   (`server/repositories/models/uuid.go:93`). Test: `make vet`. Not
   `crypto.IsConfigured` or `principal.LocalUserID`, which this list named at
   first: key rotation (`internal/app/reseal.go`) calls the one, and the
   self-service profile (`GET/PUT /api/v1/users/me`, #96) the other.
8. **Delete interface methods nothing calls, tests included,** and their
   implementations: `GetDefinitionByKey` (`contracts/definition.go:67`,
   `impl/definition.go:432`); `ListTasksByAssignee` and `ListTasksByCandidates`
   (`contracts/task.go:21-28`, `impl/task.go:67-89`); the four `UserService`
   assign methods (`contracts/user.go:36-39`, `impl/user.go:383-413`);
   `GetRootInstance` (`contracts/engine.go:58`, `impl/engine.go:235`);
   `GetPlatformUser` (`contracts/platform_user.go:18`,
   `impl/platform_user.go:40`, `impl/platform_user_unavailable.go:33`);
   `EnqueueBoundaryTimer` (`contracts/job.go:20`, `impl/job.go:798`; the runner
   keeps `executeTimerBoundary` for rows already queued); `IsComplete`
   (`contracts/adhoc_activator.go:20`, `impl/adhoc_activator.go:113`);
   `Source.Kind` (`impl/participantsource/source.go:37`,
   `impl/participantsource/http.go:31`, `impl/participantsource/postgres.go:55`);
   `ListWithFilters` with `TaskFilter`
   (`server/repositories/contracts/task.go:10-21`,
   `server/repositories/pg/task.go:72`). A test double with the extra method
   still satisfies the smaller interface. Test: `make vet`;
   `go test ./server/...`.

---

## Not worth fixing

| Report item | Why |
| :-- | :-- |
| §F: `MakeSaveEnvironmentEndpoint`, `MakeSaveAccountEndpoint`, `MakeInstallManifestEndpoint`, `MakeImportParticipantsEndpoint`, `MakeListAccountsEndpoint`, `MakeListTasksEndpoint` | Choosing one service call from the request's shape — create or update, format, kind, instance or project — is the endpoint's job. |
| §K: single-statement writes (`CreateGroup`, `DeleteUser`, `SetWebhookEnabled` and some forty more) | One statement is atomic; a unit of work around it adds nothing. |
| §K2: `jobService.callOnce` | Three commits by design: the call record must survive a failed advance (`impl/job.go:493-515`). |
| §K2: `workflowUserService.write` and `joinGroups` | An import is per row by design; a failed row is reported and a re-import repairs it (`impl/workflow_user.go:60-63`). |
| §K2: environment validate-then-write | The unique index on the port is the real guard (`server/repositories/model/environment.go:50`). |
| §K: `SSEFanout` bus writes, `sharedcount.Counter.Exchange` | Deliberately outside the caller's transaction; the bus and the counters are eventually consistent. |
| §K: `tryAcquireJobLock` | The claim is one conditional update. |
| §K: `ensureStormSchema`, `EnsureDefaultConnectors`, the two backfills | Boot-time and idempotent; one replica is the supported topology. |
| §K: staging pins the live version before allocating | The pin restates what is already live; a failed allocation leaves it true. |
| §M: pure-copy adapters | The guidelines require entities without persistence tags; a copy between the two shapes is the price. |
| §N: the facade struct and the FEEL syntax tree flagged as decorator-shaped | A composite and an interpreter's tree, not decorators. |
| §L: FEEL lexer, parser and evaluator switches; DMN hit policies; job-type dispatch | Closed sets; a switch is the simplest correct form. |
| §L2: type switches over `any` | Numeric coercion at the JSON boundary. |
| §D: `RequestExecutor`, `SharedLimiter`, `responseStarted` | Optional capabilities found by type assertion: an idiom, not ceremony. |
| §H: test hooks in `internal/pkg` (`ResetForTest`, `OverrideForTest`, `ResetDeniedSites` and others), `BuiltInConnectorKeys`, `WithMailSender`, `EnsureVersionIndexes`, `ResetEnvironmentDBs` | Documented as test seams. |
| §H: `PostgresLocker` | Kept on purpose, with the reason written down (`server/domains/services/service.go:148-155`). Revisit with the bridge in Found on the way. |
| §H: the heatmap service and its two contracts | Unreachable, but roadmap §7 item 5 plans a heatmap and an SLA report. Wire it there or delete it then. |
| §E: `App.repo`, `App.svc` | The composition root; it forwards both whole. |
| `NewServiceFacade`'s fallbacks for a nil storm-backed service | Production never reaches them, because `NewRepository` panics first; 12 of its 20 test callers pass nil. |
| Composition root reading repositories for metrics, sweeps and SSE scoping | Infrastructure reads under a system context. |
| §P, §Q, §R: largest files, structs and functions | Outside these three questions. `impl/migration.go` (1,531 lines) and `impl/engine.go` (1,255) are the first to split. |
