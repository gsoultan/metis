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
- **Scripting Engine**: Integrated **Goja** (JavaScript engine) for:
  - **Script Tasks**: Complex data transformations within workflows.
  - **Dynamic Conditions**: JavaScript-based evaluation for Gateway logic.
- **Task Inbox**: A dedicated view for users to manage, claim, and complete their assigned tasks.
- **Enterprise Persistence**:
  - **Audit Logging**: Comprehensive, persistent audit trail for every state change and node transition.
  - **Security**: **AES-256-GCM encryption** for process and task variables at rest. Requires `ENCRYPTION_KEY`; the server refuses to start without it once configured.
  - **Dual DB Support**: Supports **SQLite** for development and **PostgreSQL** for high-availability production.

## 🏗️ Architecture & Design Patterns

The project is built following **Clean Code** principles and **SOLID** design, utilizing several advanced design patterns:

- **Structural Patterns**: Facade (Service Layer), Adapter (Transports), Composite (BPMN Sub-processes), Decorator/Middleware (Logging/Auth).
- **Behavioral Patterns**: Strategy (Node Handlers), Command (Execution Steps), Observer (Event Dispatching), State (Process/Task Lifecycle), Visitor (Definition Validation), Chain of Responsibility (Condition Evaluation).
- **Creational Patterns**: Factory (Handler Creation), Builder (Test Data Setup), Singleton (DB Initialization).

## 🛠️ Technology Stack

- **Backend**: Go (1.26.5+), Go Kit, GORM, Connect RPC (gRPC-compatible).
- **Frontend**: React (19+), Vite, Mantine UI, React Flow, Zustand, TanStack Query, TanStack Router.
- **Integrations**: Goja (JS Runtime), Protobuf, AES-GCM Encryption.

## 📂 Project Structure

```text
├── api/              # Protocol Buffer definitions and generated code
├── cmd/gobpm/        # Main entry point (Server)
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

- **Go**: 1.26.5 or higher
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

### Configuration

| Variable | Purpose |
| :-- | :-- |
| `ENCRYPTION_KEY` | **Required.** Encrypts process and task variables at rest. The server refuses to start without it once configured. Rotating it makes existing variables unreadable. |
| `JWT_SECRET` | **Required** once configured. Rotating it invalidates every session. |
| `DATABASE_URL` | PostgreSQL DSN. Defaults to a local SQLite file. |
| `GOBPM_HTTP_ADDRESS` | HTTP listen address (default `:8080`). |
| `GOBPM_GRPC_ADDRESS` | gRPC listen address (default `:8081`). |
| `GOBPM_CORS_ORIGINS` | Comma-separated allowed origins, or `*`. Unset means no CORS, which is correct when the Go server serves the UI. |
| `GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS` | Allow service tasks to call loopback/RFC1918 addresses. Blocked by default to prevent SSRF via user-authored definitions. |
| `GOBPM_HTTP_ALLOWED_HOSTS` | Explicit outbound egress allowlist. |
| `GOBPM_SCRIPT_TIMEOUT` | Wall-clock budget for script tasks, gateway conditions and DMN cells (default `5s`). |
| `GOBPM_MAX_EXECUTION_DEPTH` | Nodes traversed per synchronous execution before the engine refuses to continue (default `200`). |
| `GOBPM_ALLOW_IMPLICIT_DEFAULT_FLOW` | Restores the legacy behaviour where a gateway with no matching condition took its first outgoing flow. Off by default — that silently routed processes down arbitrary branches. |
| `GOBPM_PPROF_ENABLED` | Expose pprof on `127.0.0.1:6060`. |

### Production build

```bash
make ui-build              # required: ui/embed.go embeds ui/dist
go build ./cmd/gobpm
ENCRYPTION_KEY=... JWT_SECRET=... ./gobpm
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

If nobody can sign in, reset a password from the machine running the server:

```bash
./gobpm --reset-password admin
# Password updated for "admin".
# New password: M6vY8yCdp879cTsmmWxp
```

It generates one and prints it. To choose your own without leaving it in the
shell history, pass it in the environment instead — nothing is printed then:

```bash
GOBPM_NEW_PASSWORD='...' ./gobpm --reset-password admin
```

This runs against the configured database and exits without starting a server,
so it works on an installation nobody can log into. It needs access to the
machine and the database, which is the same access a backup restore would.

## 🔌 Integrating from your application

gobpm is built to be driven by other systems: deploy definitions, start
instances, correlate messages, work human tasks from your own UI, and serve
process steps with external workers — over plain HTTP or the Go SDK:

```bash
go get github.com/gsoultan/gobpm/sdk
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