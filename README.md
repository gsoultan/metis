# Metis BPM

Metis BPM (formerly GoBPM) is a professional, production-ready BPMN orchestrator built with **Go** and **React**. It provides a powerful engine for executing complex workflows, a visual designer for modeling processes, and robust management tools.

## 🚀 Key Features

- **BPMN 2.0 Engine**: Supports essential BPMN elements including:
  - **Tasks**: User Tasks, Service Tasks (HTTP/Connectors), Script Tasks (JavaScript), Business Rule Tasks (DMN), Manual Tasks, and Call Activities.
  - **Gateways**: Exclusive, Parallel, Inclusive and Event-Based Gateways.
  - **Events**: Start, End and Terminate; Timer, Message, Signal and **Conditional** catch events; Escalation and Compensation throws; boundary events, interrupting or not.
  - **Sub-processes**: ordinary, event-triggered, and **ad-hoc** — a group of steps a person runs in whatever order the work needs, until a completion condition says it is finished.
- **BPMN XML interop**: import and export round-trip the **diagram**, not just the model — shape bounds, expanded sub-processes and connector routing — so a file from Camunda Modeler or bpmn.io keeps the layout its author drew, and a file exported from here opens in them. Execution-affecting attributes travel too: a gateway's default flow, a call activity's `calledElement`, multi-instance loop characteristics, and external-task topics written in the Camunda namespace.
- **RabbitMQ Integration**: Outbound publishes wait for a publisher confirm and are sent `mandatory`, so a message the broker cannot route is a failure rather than a silent success. Proven against a real broker in `tests/connector/broker_test.go`.
  - **Outbound Connectors**: Publish messages to RabbitMQ exchanges directly from Service Tasks.
  - **Inbound Message Correlation**: Automatically correlate RabbitMQ messages to BPMN Message Events.
  - **External Task Bridge**: Bridges External Tasks to RabbitMQ for distributed worker patterns. Unlike the outbound connector this still publishes without a confirm, so a misrouted bridge publish stalls the task until its lock expires and it is retried, rather than failing outright.
- **Connector Framework**: Plug-and-play architecture for third-party integrations (HTTP, Slack, Email, RabbitMQ).
- **Visual Designer**: Drag-and-drop BPMN modeler powered by React Flow, featuring:
  - **Edit Mode**: Load and modify existing process definitions.
  - **Property Panel**: Context-aware configuration for all BPMN nodes.
  - **Auto-Layout**: Integrated view centering for complex diagrams.
- **Asynchronous Execution**: A robust job worker system for Service Tasks and Timers with:
  - **Reliability**: Persistent job storage and execution.
  - **Error Handling**: Automatic retries with exponential backoff and jitter, then an incident when the attempts run out.
  - **Incident Management**: Capture execution failures as "Incidents" for manual resolution and retry.
- **Expressions**: **FEEL** (the DMN expression language) evaluates gateway conditions, completion conditions, input/output mappings and decision-table cells.
  - Legacy `js:` gateway conditions are **refused by default** — the JavaScript runtime cannot be memory-bounded. Installations still migrating can set `METIS_FEATURE_JAVASCRIPT_CONDITIONS=true`; `GET /api/v1/definitions/javascript-conditions` lists every stored condition that still needs rewriting.
- **Scripting Engine**: Integrated **Goja** (JavaScript engine) for **Script Tasks** — complex data transformations within workflows, under a wall-clock budget and interrupt.
- **Task Inbox**: A dedicated view for users to manage, claim, and complete their assigned tasks.
- **Process mining**: a project's audit trail exports as an **OCEL 2.0** object-centric event log (`GET /api/v1/projects/{id}/ocel`), readable by ProM, pm4py and the commercial mining tools. Process variables are excluded unless explicitly asked for — a control-flow model does not need them.
- **Enterprise Persistence**:
  - **Audit Logging**: Comprehensive, persistent audit trail for every state change and node transition.
  - **Security**: **AES-256-GCM encryption** for process and task variables at rest, and for every copy the engine keeps of them — variable history, audit trail, queued jobs, external tasks, compensation records, recorded partner responses, events broadcast between replicas, and answers kept for idempotent retries. Requires `ENCRYPTION_KEY`; the server refuses to start without it once configured. Rows written by a version before 2026-09-25 keep the form they were written in until they are next written; audit entries are never rewritten, so an older installation's history before that date stays as it was.
  - **PostgreSQL**: one engine, so a constraint has one spelling and every test runs against what production runs.
- **Topology**: job claiming, migrations, correlation, idempotency, live UI updates and rate limits are all safe across replicas. What remains per-process is circuit breakers, which open on consecutive failures by design — so a failing partner sees up to the threshold per replica before all back off, rather than in total. See [`docs/recovery.md` §2.1](docs/recovery.md) before raising the replica count.

## 🏗️ Architecture & Design Patterns

The project is built following **Clean Code** principles and **SOLID** design, utilizing several advanced design patterns:

- **Structural Patterns**: Facade (Service Layer), Adapter (Transports), Composite (BPMN Sub-processes), Decorator/Middleware (Logging/Auth).
- **Behavioral Patterns**: Strategy (Node Handlers), Command (Execution Steps), Observer (Event Dispatching), State (Process/Task Lifecycle), Visitor (Definition Validation), Chain of Responsibility (Condition Evaluation).
- **Creational Patterns**: Factory (Handler Creation), Builder (Test Data Setup), Singleton (DB Initialization).

## 🛠️ Technology Stack

- **Backend**: Go (1.27.0+), Go Kit, GORM, Connect RPC (gRPC-compatible).
- **Frontend**: React (19+), Vite, Mantine UI, React Flow, Zustand, TanStack Query, TanStack Router.
- **Integrations**: Goja (JS Runtime), Protobuf, AES-GCM Encryption.

## 📂 Project Structure

```text
├── api/              # Protocol Buffer definitions and generated code
├── cmd/metis/        # Main entry point (Server)
├── internal/pkg/     # Shared internal packages (Crypto, Logger)
├── server/           # Backend Implementation
│   ├── domains/      # Core entities and business logic
│   ├── endpoints/    # Go Kit endpoint definitions
│   ├── repositories/ # Persistence layer (GORM Models & Implementations)
│   ├── services/     # Workflow engine, handlers, and business services
│   ├── transports/   # HTTP and gRPC transport layers
│   └── interceptors/ # Centralized interceptors (Logging, Auth)
└── ui/               # Frontend React Application
    ├── src/pages/    # Designer, Task Inbox, and Admin views
    ├── src/components/ # Shared UI components and BPMN Nodes
    └── src/services/  # API client generated from Protobuf
```

## 🚦 Getting Started

### Prerequisites

- **Go**: 1.27.0 or higher
- **Bun**: https://bun.sh
- **PostgreSQL**: (Optional) For production-grade persistence

### Development

One command runs the backend and the UI together:

```bash
./scripts/dev.sh          # or: make dev
```

- UI on **http://localhost:5273**, API on **:8273**, gRPC on **:8274**
- Deliberately not 5173/8080/8081: those are what every other project on a
  developer's machine is already using. Production listens on 8080, and on a
  gRPC port only when `METIS_GRPC_ADDRESS` names one — the development script
  does. Override with `UI_PORT`, `API_PORT` or `GRPC_PORT`
- The Vite dev server proxies `/api` to the backend, so development is
  same-origin — the app talks to the server exactly as it does in production
- Development secrets are generated once into `.env.development` (gitignored)
- `Ctrl-C` stops both

```bash
./scripts/dev.sh backend    # backend only
./scripts/dev.sh ui         # UI only
./scripts/dev.sh --reset    # wipe the local database and re-run setup
./scripts/dev.sh --sample   # set up and fill it with worked examples
UI_PORT=3000 API_PORT=9000 GRPC_PORT=9001 ./scripts/dev.sh   # different ports
```

Install [air](https://github.com/air-verse/air) for backend hot reload; the
script uses it automatically when present:

```bash
go install github.com/air-verse/air@latest
```

Open the UI and the first run walks through the setup wizard. A server started
with `DATABASE_URL`, `ENCRYPTION_KEY` and `JWT_SECRET` already has its database
and keys, so the wizard asks only for the organization and first administrator
and writes no `config.yaml`. Either way it closes once the database holds an
account.

Release notes are in [`CHANGELOG.md`](CHANGELOG.md); upgrading from GoBPM is [`docs/upgrading.md`](docs/upgrading.md).

### Configuration

| Variable | Purpose |
| :-- | :-- |
| `ENCRYPTION_KEY` | **Required.** Encrypts process and task variables at rest. The server refuses to start without it once configured, and refuses a weak one — see below. Changing it alone makes existing variables unreadable; rotate it with `ENCRYPTION_KEY_PREVIOUS` and `metis --reseal` ([`docs/runbooks.md`](docs/runbooks.md), "Rotating secrets"). |
| `ENCRYPTION_KEY_PREVIOUS` | The key being rotated away from. Read with, never written with, so data sealed under it still opens while `metis --reseal` seals it again under `ENCRYPTION_KEY`. Remove it once `metis --reseal-check` passes. |
| `JWT_SECRET` | **Required** once configured. Rotating it invalidates every session. A weak one is forgeable into an administrator's token. |
| `METIS_ALLOW_WEAK_SECRETS` | Start anyway with a secret that would be refused, while a weak `ENCRYPTION_KEY` is rotated to a strong one; warns on every boot. |
| `DATABASE_URL` | PostgreSQL DSN. Required unless `config.yaml` names a database; there is no local-file fallback, because one that appears silently is one somebody starts using and then loses. |
| `METIS_HTTP_ADDRESS` | HTTP listen address (default `:8080`). |
| `METIS_GRPC_ADDRESS` | gRPC listen address. **Unset means no gRPC listener**, which is the default: it applies none of the HTTP chain — no authentication, rate or body limit — so only the calls that need no sign-in answer on it. The same services are served over HTTP through Connect. |
| `METIS_CORS_ORIGINS` | Comma-separated allowed origins, or `*`. Unset means no CORS, which is correct when the Go server serves the UI. |
| `METIS_HTTP_ALLOW_PRIVATE_NETWORKS` | Allow service tasks to call loopback/RFC1918 addresses. Blocked by default to prevent SSRF via user-authored definitions. |
| `METIS_HTTP_ALLOWED_HOSTS` | Explicit outbound egress allowlist. |
| `METIS_SCRIPT_TIMEOUT` | Wall-clock budget for script tasks, gateway conditions and DMN cells (default `5s`). |
| `METIS_SCRIPT_CONCURRENCY` | How many scripts may execute at once (default `4`, below `METIS_JOB_WORKERS`). The sandbox bounds a script's wall-clock time, recursion and host capability, and it cannot bound its memory — goja exposes no heap limit — so the quantity that *is* bounded is how many scripts allocate simultaneously. A slot is held until the script actually finishes, so a runaway that ignored its interrupt keeps being counted; when every slot is held, further scripts are refused with a message naming this variable rather than piling up. Set the container memory limit as the backstop ([`deploy/kubernetes/`](deploy/kubernetes/) uses `1Gi`): a pod the kernel kills is retried on its lease, a node that degrades is not. |
| `METIS_MAX_EXECUTION_DEPTH` | Nodes traversed per synchronous execution before the engine refuses to continue (default `200`). |
| `METIS_FEATURE_JAVASCRIPT_CONDITIONS` | Allow `js:` gateway conditions. **Off by default** — goja cannot be pre-empted mid-call (measured: 37s against a 200ms budget), so authored JavaScript is a memory-exhaustion vector FEEL does not have. Turn on only while migrating; `GET /api/v1/definitions/javascript-conditions` is the worklist. |
| `METIS_FEATURE_STRICT_TENANT_SCOPE` | Make a repository query carrying neither a tenant nor a system identity return nothing instead of everything. Off by default pending a staged rollout — its failure mode is silence, not an error. Seven suites covering the real interceptor chain pass under it (`make strict-scope`). [`docs/strict-tenant-scope.md`](docs/strict-tenant-scope.md) is the rollout. |
| `METIS_ALLOW_IMPLICIT_DEFAULT_FLOW` | Restores the legacy behaviour where a gateway with no matching condition took its first outgoing flow. Off by default — that silently routed processes down arbitrary branches. |
| `METIS_TRUSTED_PROXIES` | Which peers may set `X-Forwarded-For`, as comma-separated CIDRs. Defaults to loopback and private space, which is where a load balancer or sidecar connects from. Set it to `none` when the server is exposed directly. **Requests from anywhere else have the header ignored** — it is a client-set header, and believing it unconditionally let one address take 30 requests through a limit of 3 by varying it. |
| `METIS_PPROF_ENABLED` | Expose pprof on `127.0.0.1:6060`. |
| `METIS_DB_MAX_OPEN_CONNS` | Connection pool ceiling (default `25`), applied to each of the process's two pools (GORM and storm), so a process opens up to twice this. Previously unset, which means *unlimited* — a burst could open more connections than PostgreSQL's default `max_connections` of 100 and fail every caller at once. |
| `METIS_DB_MAX_IDLE_CONNS` | Idle connections kept open (defaults to the open ceiling). The `database/sql` default of 2 closes the rest as soon as a burst subsides and pays a fresh handshake on the next one. |
| `METIS_DB_CONN_MAX_LIFETIME` | How long a connection may live (default `30m`). Bounded so a database failover or rolling restart is picked up without restarting Metis. |
| `METIS_DB_CONN_MAX_IDLE_TIME` | How long an unused connection is kept (default `5m`). |
| `METIS_DEFINITION_CACHE_SIZE` | Decoded process definitions held in memory (default `256`). A definition is immutable once deployed but is read on every job, message and timer; the cache is bounded, evicts least-recently-used, and is keyed by tenant so a cached copy can never cross an organization boundary. |
| `METIS_JOB_WORKERS` | Jobs run at once (default `10`). Was a compile-time 5, which with a fixed 2-second poll capped a replica at about 2.5 jobs a second whatever the hardware. Keep it at or below the database pool: above that, workers queue on connections instead of working. |
| `METIS_JOB_POLL_INTERVAL` | How long an idle worker waits before looking again (default `2s`). It bounds how late the *first* job of a quiet period starts; once work exists the worker keeps claiming without waiting. |
| `METIS_JOB_LEASE` | How long a claim is held before another worker may take the job (default `5m`). Values under 2 minutes are refused: an outbound call may run for 30 seconds, and a lease shorter than that permits a second worker to run a job still in flight, which is a duplicate service call. |
| `METIS_AUTH_CACHE_TTL` | How long a resolved caller is reused (default `5s`, `0s` disables). Validating a token read the account twice with associations preloaded — about six queries before a request reached its handler. Deliberately seconds: the cached value carries the credential cutoff that invalidates tokens, so a stale entry extends a compromised session. Password, role and membership changes drop the entry immediately. |
| `METIS_AUTH_CACHE_SIZE` | Accounts held (default `10000`, evicting least-recently-used). |
| `METIS_REFUSE_SCHEMA_DRIFT` | Refuse to start when a model change has no migration (default off, a warning). The drift count is published as `metis_schema_drift_items` and alerted on either way — features behind a missing table return 500 while `/readyz` stays green, so this is not otherwise visible. Useful in staging to fail the deploy rather than discover it in production. |
| `METIS_SHUTDOWN_DRAIN` | How long a stopping process waits for jobs it has already claimed (default `20s`). Shutdown used to abandon them: the final status write rode the cancelled context and failed, so the row kept its lock until the five-minute lease expired — and the shipped manifest uses a Recreate strategy, so that was every deploy. Keep it inside your termination grace period. |
| `METIS_HTTP_MAX_RESPONSE_BYTES` | Ceiling on a single outbound reply a service task or connector reads into memory (default `8388608`, 8 MiB). Exceeding it is refused rather than truncated: a process must not act on half a document it believes is whole. Without a bound, a partner streaming an unbounded body exhausts the pod's memory. |

### Production build

The image is the supported artifact. It builds the UI, compiles a static binary
and ships it on a distroless base with no shell — one file plus certificates,
running as a non-root user against a read-only root filesystem.

Every tagged release also publishes **Linux archives** — `amd64` and `arm64`,
each a static binary with the UI already embedded — for anyone putting Metis on
a host under systemd rather than in a scheduler. They are the same build as the
image, stamped with the same version, and carry a bill of materials, build
provenance and a `checksums.txt`. There is no macOS or Windows build: Metis
compiles on both and nothing tests it there, and publishing a binary is a claim
to support it.

```bash
make docker                # stamps the image with `git describe`
```

Try it, with a PostgreSQL alongside:

```bash
make docker-run            # http://localhost:8080
```

For Kubernetes, [`deploy/kubernetes/`](deploy/kubernetes/) is a complete
deployment rather than a skeleton — read-only root, non-root user, the right
probe on the right endpoint, and comments saying what each field prevents.

Both secrets must be at least 32 characters, and must not be one of the
placeholders published in this repository. Generate them:

```bash
openssl rand -hex 24     # ENCRYPTION_KEY
openssl rand -base64 48  # JWT_SECRET
```

The server refuses to start otherwise. A weak secret is not a degraded mode —
it behaves exactly like a strong one until somebody guesses it offline, and
then it is total: a `JWT_SECRET` becomes an administrator's token, an
`ENCRYPTION_KEY` turns a stolen backup back into plaintext. If you are upgrading
an installation with a weak `ENCRYPTION_KEY`, rotate it to a strong one — the
old key goes in `ENCRYPTION_KEY_PREVIOUS` and `metis --reseal` moves the data
([`docs/runbooks.md`](docs/runbooks.md), "Rotating secrets").
`METIS_ALLOW_WEAK_SECRETS=true` starts it on the weak key in the meantime.

`docker-compose.yml` is for evaluation: the secrets in it are literals. Generate
real ones for anything else, and back `ENCRYPTION_KEY` up separately from the
database — a backup without it restores unreadable rows
([`docs/recovery.md`](docs/recovery.md)).

The published image is `ghcr.io/gsoultan/metis:v0.3.0`, for `linux/amd64` and `linux/arm64`. Pin a digest for a real
deployment — a moving tag makes a rollback ambiguous, which is the one moment it
needs not to be.

Ask a running server which build it is:

```bash
curl -s localhost:8080/healthz     # {"status":"ok","version":"v1.2.3"}
```

Building without Docker, which is also what CI does:

```bash
make ui-build              # required: ui/embed.go embeds ui/dist
go build ./cmd/metis
ENCRYPTION_KEY=... JWT_SECRET=... ./metis
```

### Something to look at

A new installation is empty, which shows that it works but not what it does.
To start it already carrying the examples from `docs/data-flow.md`:

```bash
./scripts/dev.sh --sample          # or --reset --sample to start over
```

That runs the setup wizard for you, imports an expense approval and a new
supplier check with the decision tables they consult, creates the people and
groups those approvals are offered to, and starts five instances — so the
process list, the decision list, the instance list, the task inbox and the
incident inbox all have something in them.

Sign in as `admin` / `admin`, which belongs to every group and so sees
everything. The others use the password `sample-password`:

| Account | Sees |
| :-- | :-- |
| `manager` | Approvals under GBP 1,000 |
| `director` | Approvals above it |
| `reviewer` | The supplier compliance review, once the incident below is resolved |

The amount decides who approves: under 100 needs nobody, under 1000 a manager,
anything more a director. `docs/data-flow.md` follows one through, value by
value.

One supplier check **fails on purpose** — its first step calls a web address
that does not exist. It retries with backoff first, so give it a couple of
minutes; the incident appears once the retries are spent. Then open Instances,
pick the failed one and choose "Show what failed" to read the cause and retry
it. That is the operator's half of the product, and showing it needs something
broken.

To seed an installation that is already set up, give it the password you chose:

```bash
SAMPLE_ADMIN_PASS='...' ./scripts/seed-sample.sh
```

### Signing in, and getting back in

There is no default account. The administrator username and password are the
ones typed into the setup wizard on first run — `admin/admin` works only if that
is what you chose.

Signed-in users change their own password from **Profile → Change Password**,
which asks for the current one — a session alone is not enough, so a stolen
token cannot lock the owner out of their own account. **Changing it ends every
session, including the one making the change**, because the usual reason to
change a password is that somebody else may have it. The same applies to
`--reset-password`. Accounts that sign in through OIDC have no password here;
theirs lives at the identity provider.

If nobody can sign in at all, reset one from the machine running the server:

```bash
./metis --reset-password admin
# Password updated for "admin".
# New password: M6vY8yCdp879cTsmmWxp
```

It generates one and prints it. To choose your own without leaving it in the
shell history, pass it in the environment instead — nothing is printed then:

```bash
METIS_NEW_PASSWORD='...' ./metis --reset-password admin
```

This runs against the configured database and exits without starting a server,
so it works on an installation nobody can log into. It needs access to the
machine and the database, which is the same access a backup restore would.

## 🔌 Integrating from your application

Metis is built to be driven by other systems: deploy definitions, start
instances, correlate messages, work human tasks from your own UI, and serve
process steps with external workers — over plain HTTP or the Go SDK:

```bash
go get github.com/gsoultan/metis-sdk
```

The SDK has no dependencies outside the Go standard library and lives in its
own repository, [gsoultan/metis-sdk](https://github.com/gsoultan/metis-sdk),
so it versions independently of the server. Start with
[`docs/integration.md`](docs/integration.md); the SDK's `examples/quickstart`
runs the whole journey against a live server.

## 🧪 Testing

Run the full verification gate (build, vet, tests, race, UI typecheck/lint/build):
```bash
make gate
```

Individual steps:
```bash
make test    # full Go suite — note ./server/... alone SKIPS the tests/ tree
make race    # race detector
make vet     # go vet, module-wide
```

## 🔒 Security

Reporting a vulnerability, what counts as untrusted input, and which
alarming-looking decisions are deliberate: [`SECURITY.md`](SECURITY.md). It also
lists what has already been audited, so a reviewer does not re-tread it.

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.