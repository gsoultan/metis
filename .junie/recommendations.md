### Metis: what these recommendations became

This file was a wishlist written against an earlier engine. Every item on it has
since shipped, so it is kept as a record of where each one landed rather than as
open work — a contributor reading the old version could reasonably have gone and
built FEEL a second time.

Open work lives in [`roadmap.md`](roadmap.md), which is the file to read for what
to do next. The priority order there is fixed: P0 Security & Reliability →
P1 Scalability & Performance → P2 UX Delight.

| Was recommended | Where it lives now |
| :-- | :-- |
| **Multi-instance** — parallel and sequential loops | `NodeHandlerTemplate.handleMultiInstance` (`server/domains/handlers/impl/template.go`), covered by `tests/bpmn/multi_instance_test.go` and, for the concurrent case, `tests/postgres/multi_instance_concurrency_test.go` |
| **Integrated form builder** | `ui/src/components/FormBuilder.tsx`, rendered for a user task by `TaskForm.tsx` and configured from `properties/UserTaskConfig.tsx` |
| **Deployment lifecycle** — immutable versions, migrating running instances | Versions are allocated per project and never rewritten (`NextVersion`); migrating live instances between them is `server/domains/services/impl/migration.go` |
| **Enhanced DMN / FEEL instead of JavaScript** | `server/domains/logic/feel` is a real lexer, parser and evaluator, not a string matcher. It went further than the recommendation: `js:` conditions are now **refused by default**, and `GET /api/v1/definitions/javascript-conditions` is the migration worklist |
| **History view** | `BusinessTimeline.tsx` over the persistent audit trail |
| **Horizontal scaling** — distributed locking | `server/domains/services/impl/postgres_lock.go`. **Read the caveat before relying on it:** a single replica is still the supported topology. Advisory locks are session-scoped, so the locker pins a connection per lock; and two components (HTTP rate limiting, connector rate limits and breakers) still hold per-process state. `docs/recovery.md` §2.1 has the table |
