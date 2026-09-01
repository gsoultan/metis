### Metis Project Guidelines

#### 1. General Principles (KISS & Clean Code)
- **Keep it Simple (KISS)**: MUST favor clear, readable code over clever/complex solutions. No over-engineering.
- **Consistency**: MUST follow existing patterns, naming conventions, and file structures.
- **Minimalism**: MUST keep implementations lean; MUST remove dead code. AVOID unnecessary abstractions, layers, and dependencies.
- **Clean Code Patterns**:
    - **Early Returns**: MUST use guard clauses and early returns to handle edge cases/errors first. Keep the "happy path" at the lowest indentation level.
    - **Function Scope**: MUST keep functions small and focused (aim for one screenful). Refactor complex branches into named functions.
    - **Loop & Indentation Management**:
        - **Extract Inner Loops**: MUST extract the inner logic of complex loops into a dedicated named function or a local helper.
        - **Flatten Data with Maps**: MUST favor using maps for lookups to turn $O(n^2)$ nested loops into sequential $O(n)$ loops.
        - **Avoid "Pyramid of Doom"**: MUST keep nesting level to a minimum (ideally 2-3 levels) by using early returns and extracting logic.
    - **Descriptive Naming**: MUST use intention-revealing names for variables, functions, and types. AVOID cryptic abbreviations.
    - **No Magic Values**: MUST use named constants instead of literal numbers or strings.
- **SOLID Design**: MUST apply Single Responsibility, Open/Closed, Liskov Substitution, Interface Segregation, and Dependency Inversion.
- **Design Patterns**: Use established design patterns (Strategy, Factory, Observer, Middleware, Singleton, Builder, Decorator, Adapter, Proxy, Facade, Chain of Responsibility, Mediator, State, Command, Interpreter, Iterator, Memento, Visitor, Null Object, Template Method, Repository, Abstract Factory, Prototype, Flyweight, Composite, Bridge) to solve structural/behavioral challenges.

#### 2. Architecture & Interface Design (MANDATORY)
- **Programming by Interface**: MUST depend on interfaces rather than concrete implementations for ALL cross-boundary interactions (service↔service, service↔repository, handler↔service, adapter↔external system).
- **One File, One Type**: MUST place each interface, struct, or type in its own dedicated file. NEVER group multiple unrelated types in one file.
- **No God Structures**: MUST NOT create God structs (structs with >5 unrelated responsibilities), God interfaces (interfaces with >5 methods), God folders (folders with >15 files of mixed concerns), or God files (files with >200 lines covering multiple responsibilities).
- **Composition Interfaces**: When an interface naturally needs >5 methods, MUST break it into smaller focused interfaces and compose them:
  ```go
  // BAD — God interface
  type TaskService interface { Create, Get, List, Update, Delete, Claim, Unclaim, Complete, Assign, Escalate }

  // GOOD — Composed focused interfaces
  type TaskReader interface { Get(ctx, id) (*Task, error); List(ctx, filter) ([]*Task, error) }
  type TaskWriter interface { Create(ctx, cmd) (*Task, error); Update(ctx, cmd) (*Task, error) }
  type TaskClaimer interface { Claim(ctx, id, userID) error; Unclaim(ctx, id) error }
  type TaskService interface { TaskReader; TaskWriter; TaskClaimer }
  ```
- **Interface Segregation (ISP)**: MUST define consumer-centric interfaces at the point of use. Never force a consumer to depend on methods it does not use.
- **Small, Focused Functions**: MUST keep every function under ~40 lines (one screenful). If a function grows beyond that, MUST extract sub-logic into named helper functions with intention-revealing names.
- **Design Patterns — Required Usage**:
  - **Strategy**: Use for swappable algorithms (locking backends, expression evaluators, broker adapters).
  - **Factory / Abstract Factory**: Use for creating domain objects and connector instances.
  - **Observer**: Use for all event-driven cross-domain notifications.
  - **Decorator**: Use for adding cross-cutting concerns (logging, metrics, retry) to service/repository implementations.
  - **Repository**: Use for all data access; hide ORM/SQL details behind the contract interface.
  - **Unit of Work**: Use for multi-step write operations requiring atomicity.
  - **Chain of Responsibility**: Use for middleware pipelines and BPMN flow execution chains.
  - **Command**: Use for encapsulating BPMN execution steps as discrete command objects.
  - **Adapter**: Use to bridge external systems (brokers, connectors, DMN engines) to internal domain ports.
  - **Facade**: Use `ServiceFacade` as the single orchestration entry-point; keep it thin (delegate only).
  - **Null Object**: Use to provide safe default no-op implementations of optional interfaces.

#### 3. Go Guidelines (1.24+)
- **Standard Formatting**: MUST use `gofmt` and `goimports`.
- **Idioms (Modern Go)**:
    - Use `any` instead of `interface{}`.
    - Use `errors.Is` and `errors.As` for error checking and unwrapping.
    - Use `slices.*` (Contains, Max, IndexFunc, etc.) and `maps.*` (Clone, Copy, Keys, etc.).
    - Use built-in `min/max`, `clear`, `cmp.Or`, and `sync.OnceFunc`.
    - Use `omitzero` JSON tag for time, structs, slices, and maps (Go 1.24+).
- **Error Handling**: MUST handle all errors explicitly. Wrap with context using `%w`.
- **Concurrency**: MUST use `wg.Go()` for `WaitGroups` (assuming a helper or `errgroup`). MUST use `t.Context()` in tests (Go 1.24+).
- **Testing**:
    - MUST write unit tests for new logic using the standard `testing` package.
    - MUST use table-driven tests for multiple test cases.
    - MUST specify a reasonable timeout for all tests (e.g., using `context.WithTimeout`) to prevent hangs.
    - MUST use `db.SetMaxOpenConns(1)` when using SQLite (especially `:memory:`) in tests to ensure deterministic behavior.
- **Documentation**: Use standard Go doc comments for exported symbols. Explain "why" for non-obvious decisions.
- **OOP & Composition**:
    - **Programming by Interface**: MUST depend on interfaces rather than concrete implementations for all service-to-service, service-to-repository, and cross-domain interactions.
    - MUST prefer composition to inheritance.
    - MUST use small interfaces (1–3 methods) close to consumers.
    - MUST separate interfaces from their implementations into different files.
    - **Composition over God Structs**: Embed specialized smaller structs into a main struct to maintain a unified API while delegating logic to manageable units.
    - **Consumer-Centric Interfaces**: Define interfaces that serve the consumer's needs rather than the implementation.
    - Use unexported fields with exported getter/setter methods where appropriate.
    - Use pointer receivers (`*Type`) for mutating methods.
- **Domain & Model Organization**:
    - MUST move domain entities to a dedicated `domains/entities` folder.
    - MUST move database models (e.g., GORM models) to a dedicated `repositories/models` folder.
    - MUST move repository interfaces to a dedicated `repositories/contracts` folder.
    - MUST move all service interfaces (except `ServiceFacade`) to a dedicated `services/contracts` folder and separate based on domain-specific. `ServiceFacade` MUST be placed in the `services` folder.
    - MUST move handler interfaces to a dedicated `domains/handlers/contracts` folder and separate based on domain-specific.
    - MUST move observer interfaces to a dedicated `domains/observers/contracts` folder and separate based on domain-specific.
    - MUST move GORM repository implementations to a dedicated `repositories/gorms` folder.
    - MUST separate database models, repository contracts, and repository implementations into individual files per database table (e.g., `user.go` for the `users` table) within `repositories/models`, `repositories/contracts`, and `repositories/gorms`.
    - MUST move Go kit endpoints to a dedicated `endpoints/{domain-specific}` folder.
    - MUST separate request/response structs into a dedicated `request_response.go` file within the same folder.
    - MUST separate endpoint implementation into a dedicated `endpoint.go` file within the same folder.
    - MUST move interceptor files to a dedicated `interceptors` folder under `server`.
    - MUST separate entity, model, contract, GORM implementation, endpoint, middleware, handler, and observer files based on domain-specific entities (e.g., `process.go`, `task.go`, `auth.go`).
    - MUST use plural names for `domains`, `entities`, `repositories`, `models`, `contracts`, `gorms`, and `endpoints` folders.
    - MUST use UUID V7 for all primary key IDs.
    - **Object-Oriented Entities**: MUST ensure domain entities are Object-Oriented. All relational field references MUST be pointers to other entities (objects) instead of IDs (`uuid.UUID`). Use "shell" objects (objects with only the ID set) in adapters when full objects are not available.
    - **Separation of Concerns (Entities)**: MUST NOT include database-specific (e.g., GORM) tags in domain entities. Database mappings MUST be handled in separate model structs (under `repositories/models`) and adapters.

#### 3. Backend & SQL
- **SQL Management**: MUST move all SQL queries to a dedicated `queries.go` file within the package.
- **Query Registry**: MUST use a `queryRegistry` or similar pattern to manage common queries and driver-specific overrides.
- **Portability**: MUST ensure compatibility with SQLite, MySQL, Postgres, and SQL Server.
- **Placeholders**: MUST use driver-neutral placeholders (`?`) and implement a translation layer if needed (e.g., `$1` for Postgres).
- **Security & Migrations**: MUST NOT log secrets or PII. Use parameterized queries. Ensure migrations are idempotent (`IF NOT EXISTS`).
- **Transaction Pattern (Unit of Work)**:
    - **Consolidate Multi-Step Writes**: MUST wrap service methods performing multiple repository calls (especially writes) in `s.repo.UnitOfWork().Do(ctx, ...)`.
    - **Protect Read-Modify-Write Cycles**: MUST use transactions for operations like version incrementing to prevent race conditions.
    - **Transactional Commands**: MUST ensure BPMN execution steps are atomic by wrapping command execution in transactions.
    - **Atomic Dispatching**: MUST ensure `DispatchEvent` is part of the same transaction as the action that triggered it to maintain audit log consistency.
    - **Simple Updates**: SHOULD wrap simple `Get` followed by `Update` operations in a transaction (potentially with `GetForUpdate`) to ensure consistency.

#### 4. UI Guidelines (React/TS/Zustand)
- **Routing**: MUST use `@tanstack/react-router` for all navigation. MUST use file-based routing in the `ui/src/routes` directory. MUST use `Link` components for internal navigation. MUST use `validateSearch` for type-safe search parameters.
- **Lazy Loading**: MUST use `React.lazy` and `Suspense` for heavy components. Routing-level lazy loading is handled by TanStack Router.
- **Optimized Rendering**:
    - MUST derive state during rendering; AVOID storing derived data in state or using `useEffect` to sync it.
    - MUST use `useTransition` for non-urgent updates (e.g., filtering lists).
    - MUST parallelize independent operations using `Promise.all()`.
    - MUST use explicit boolean checks for conditional rendering (`count > 0 && ...`).
- **State Management**:
    - Global: MUST use `Zustand`.
    - Server: MUST use `@tanstack/react-query` (caching, retries, deduping).
    - Local: `useState`/`useReducer`.
- **React Design Patterns**:
    - **Custom Hooks**: Primary pattern for reusing stateful logic (e.g., `useFetchData`).
    - **Provider Pattern**: Use Context API for global data (themes, auth).
    - **Container/Presentational**: Separate logic (fetching) from UI (rendering).
    - **Compound Components**: Flexible API for related components (e.g., `<Select.Option>`).
- **Data Fetching**: MUST use `useQuery`. MUST abort in-flight requests on unmount/param changes.
- **Performance**:
    - MUST virtualize long lists; MUST use stable `key` props.
    - MUST offload CPU-bound tasks (JSON diffing, filtering) to Web Workers.
    - MUST use dynamic `import()` to code-split the application.
    - MUST use `build.rollupOptions.output.manualChunks` in `vite.config.ts` to improve chunking.
    - MUST adjust chunk size limit for warnings via `build.chunkSizeWarningLimit` in `vite.config.ts`.
- **Verification**: MUST ensure `go run ./cmd/metis --build-ui` (which runs `bun run build` in the `ui` directory) passes successfully after any UI changes.
- **CI/CD Readiness**: NEVER submit changes that break the UI build or the main Go entry point's ability to build the UI.

#### 5. Low-Code & Non-Expert Friendliness
- **Human-Friendly Activity Feed**: MUST replace technical logs with human-readable "Business Timeline" narratives (e.g., "The manager approved the request" instead of "Task_Approved").
- **Low-Code Form Logic**: MUST use visual condition builders for form field visibility and validation (e.g., "Hide [Field A] IF [Field B] is empty").
- **Visual Data Mapper**: MUST favor visual drag-and-drop mapping for service task inputs/outputs instead of manual JSON key entries.
- **Smart Troubleshooter**: MUST provide plain English error messages with specific "Quick-Fix" suggestions (e.g., "Slack token expired. Click here to refresh").
- **Progressive Disclosure**: MUST hide advanced technical settings by default and only show them when requested by an "Expert Mode" toggle.

#### 6. Roadmap Execution Governance
- **Single Source of Truth**: MUST keep `.junie/roadmap.md` updated as the canonical enhancement roadmap.
- **Execution Discipline**: MUST convert roadmap themes into concrete, testable backlog items with measurable acceptance criteria.
- **Priority Order**: MUST execute in this order unless explicitly reprioritized: `P0 Security & Reliability` → `P1 Scalability & Performance` → `P2 UX Delight`.
- **Evidence Requirement**: A recommendation is considered "executed" only if code/config/docs were updated and verification evidence (tests/build/run output) is available.
- **Incremental Delivery**: MUST prefer small, reversible changes with feature flags/canary strategy for risky behavior changes.
