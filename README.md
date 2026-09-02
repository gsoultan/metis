# Metis BPM

Metis BPM (formerly GoBPM) is a professional, production-ready BPMN orchestrator built with **Go** and **React**. It provides a powerful engine for executing complex workflows, a visual designer for modeling processes, and robust management tools.

## 🚀 Key Features

- **BPMN 2.0 Engine**: Supports essential BPMN elements including:
  - **Tasks**: User Tasks, Service Tasks (HTTP/Connectors), Script Tasks (JavaScript), and Call Activities (sub-processes).
  - **Gateways**: Exclusive and Parallel Gateways.
  - **Events**: Start, End, and Intermediate Timer/Message Catch Events.
- **RabbitMQ Integration**: Production-ready messaging capabilities:
  - **Outbound Connectors**: Publish messages to RabbitMQ exchanges directly from Service Tasks.
  - **Inbound Message Correlation**: Automatically correlate RabbitMQ messages to BPMN Message Events.
  - **External Task Bridge**: Seamlessly bridge External Tasks to RabbitMQ for distributed worker patterns.
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
- **Enterprise Persistence**:
  - **Audit Logging**: Comprehensive, persistent audit trail for every state change and node transition.
  - **Security**: **AES-256-GCM encryption** for process and task variables at rest. Requires `ENCRYPTION_KEY`; the server refuses to start without it once configured.
  - **Dual DB Support**: Supports **SQLite** for development and **PostgreSQL** for production.
- **Topology**: a **single engine replica** is still the supported deployment. Job claiming, migrations, correlation, idempotency and live UI updates are safe across replicas; what remains per-process is HTTP rate limiting and connector rate limits/circuit breakers, so with N replicas each limit is applied N times over — see [`docs/recovery.md` §2.1](docs/recovery.md) for the full table and what each one costs.

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

- UI on **http://localhost:5273**, API on **:8080**, gRPC on **:8081**
- The Vite dev server proxies `/api` to the backend, so development is
  same-origin — the app talks to the server exactly as it does in production
- Development secrets are generated once into `.env.development` (gitignored)
- `Ctrl-C` stops both

```bash
./scripts/dev.sh backend    # backend only
./scripts/dev.sh ui         # UI only
./scripts/dev.sh --reset    # wipe the local database and re-run setup
UI_PORT=3000 API_PORT=9000 ./scripts/dev.sh    # different ports
```

Install [air](https://github.com/air-verse/air) for backend hot reload; the
script uses it automatically when present:

```bash
go install github.com/air-verse/air@latest
```

Open the UI and the first run walks through the setup wizard.

Release notes are in [`CHANGELOG.md`](CHANGELOG.md); upgrading from GoBPM is [`docs/upgrading.md`](docs/upgrading.md).

### Configuration

| Variable | Purpose |
| :-- | :-- |
| `ENCRYPTION_KEY` | **Required.** Encrypts process and task variables at rest. The server refuses to start without it once configured, and refuses a weak one — see below. Rotating it makes existing variables unreadable. |
| `JWT_SECRET` | **Required** once configured. Rotating it invalidates every session. A weak one is forgeable into an administrator's token. |
| `METIS_ALLOW_WEAK_SECRETS` | Start anyway with a secret that would be refused. For an existing installation that cannot rotate `ENCRYPTION_KEY` without losing data; warns on every boot. |
| `DATABASE_URL` | PostgreSQL DSN. Defaults to a local SQLite file. |
| `METIS_HTTP_ADDRESS` | HTTP listen address (default `:8080`). |
| `METIS_GRPC_ADDRESS` | gRPC listen address (default `:8081`). |
| `METIS_CORS_ORIGINS` | Comma-separated allowed origins, or `*`. Unset means no CORS, which is correct when the Go server serves the UI. |
| `METIS_HTTP_ALLOW_PRIVATE_NETWORKS` | Allow service tasks to call loopback/RFC1918 addresses. Blocked by default to prevent SSRF via user-authored definitions. |
| `METIS_HTTP_ALLOWED_HOSTS` | Explicit outbound egress allowlist. |
| `METIS_SCRIPT_TIMEOUT` | Wall-clock budget for script tasks, gateway conditions and DMN cells (default `5s`). |
| `METIS_MAX_EXECUTION_DEPTH` | Nodes traversed per synchronous execution before the engine refuses to continue (default `200`). |
| `METIS_FEATURE_JAVASCRIPT_CONDITIONS` | Allow `js:` gateway conditions. **Off by default** — goja cannot be pre-empted mid-call (measured: 37s against a 200ms budget), so authored JavaScript is a memory-exhaustion vector FEEL does not have. Turn on only while migrating; `GET /api/v1/definitions/javascript-conditions` is the worklist. |
| `METIS_FEATURE_STRICT_TENANT_SCOPE` | Make a repository query carrying neither a tenant nor a system identity return nothing instead of everything. Off by default pending a staged rollout — its failure mode is silence, not an error. Seven suites covering the real interceptor chain pass under it (`make strict-scope`). [`docs/strict-tenant-scope.md`](docs/strict-tenant-scope.md) is the rollout. |
| `METIS_ALLOW_IMPLICIT_DEFAULT_FLOW` | Restores the legacy behaviour where a gateway with no matching condition took its first outgoing flow. Off by default — that silently routed processes down arbitrary branches. |
| `METIS_TRUSTED_PROXIES` | Which peers may set `X-Forwarded-For`, as comma-separated CIDRs. Defaults to loopback and private space, which is where a load balancer or sidecar connects from. Set it to `none` when the server is exposed directly. **Requests from anywhere else have the header ignored** — it is a client-set header, and believing it unconditionally let one address take 30 requests through a limit of 3 by varying it. |
| `METIS_PPROF_ENABLED` | Expose pprof on `127.0.0.1:6060`. |

### Production build

The image is the supported artifact. It builds the UI, compiles a static binary
and ships it on a distroless base with no shell — one file plus certificates,
running as a non-root user against a read-only root filesystem.

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
an installation that cannot rotate its key, `METIS_ALLOW_WEAK_SECRETS=true`
starts it while you plan a re-encryption.

`docker-compose.yml` is for evaluation: the secrets in it are literals. Generate
real ones for anything else, and back `ENCRYPTION_KEY` up separately from the
database — a backup without it restores unreadable rows
([`docs/recovery.md`](docs/recovery.md)).

The published image is `ghcr.io/gsoultan/metis:0.1.0`. Pin a digest for a real
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
supplier check with the decision tables they consult, and starts four
approvals — so the process list, the decision list, the instance list and the
task inbox all have something in them. Sign in as `admin` / `admin`.

The amount decides who approves: under 100 needs nobody, under 1000 a manager,
anything more a director. `docs/data-flow.md` follows one through, value by
value.

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
token cannot lock the owner out of their own account. Accounts that sign in
through OIDC have no password here; theirs lives at the identity provider.

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
go get github.com/gsoultan/metis/sdk
```

The SDK has no dependencies outside the Go standard library. Start with
[`docs/integration.md`](docs/integration.md); `sdk/examples/quickstart` runs
the whole journey against a live server.

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

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.