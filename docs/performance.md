# Performance

How Metis is measured, the numbers it has shown, and how to profile it when a
number moves. The targets are the roadmap's §1 and they do not relax as data
grows: a read p95 under **150ms**, a workflow action p95 under **500ms**, under
**0.1%** of responses as 5xx, and **10,000** process starts a minute.

## Where each target is measured

| Where | What it proves | When it runs |
| :--- | :--- | :--- |
| `tests/slo` | The HTTP handler meets the targets in-process, with one to two orders of magnitude of headroom. It catches a regression that costs an order of magnitude, such as an N+1 or a per-request compile — and, counted rather than timed, a request that reads its organization's project list more than once. | Every `make test` |
| `tests/loadtest` | The same targets hold with production-shaped volume across tenants. It catches a plan that flips at scale, which ten rows cannot show. | On request: `METIS_LOADTEST=1 go test ./tests/loadtest/ -v -timeout 30m`, sized with `METIS_LOADTEST_INSTANCES` and `METIS_LOADTEST_TENANTS` |
| `tests/loadtest`, concurrent writes | Many people completing work at once, on a process that splits into two approvals, joins, and calls a partner. Every instance finishes once, every task is completed once, nothing is left at the join, and the partner is called once per instance under its own key. A second submission of a completion is refused with a 400, and nothing else may fail. The action target is asserted; throughput is reported. | On request: `METIS_LOADTEST=1 go test ./tests/loadtest/ -run ConcurrentApprovals -v`, sized with `METIS_LOADTEST_WRITE_INSTANCES` (200) and `METIS_LOADTEST_WRITE_WORKERS` (16). `METIS_LOADTEST_SEED` replays an order of submissions |
| `tests/loadtest`, tenant scope | One organization with 10,000 projects against one with 4, the same instances and tasks in each: the p95 of the reads a person opens the product with, how often each request reads its organization's project list, and what each request allocates. See *Tenant scope at ten thousand projects* below. | On request: `METIS_LOADTEST=1 go test ./tests/loadtest/ -run TenantScope -v`, sized with `METIS_LOADTEST_SCOPE_PROJECTS` (10,000) and `METIS_LOADTEST_SCOPE_INSTANCES` (5,000 per busy project) |
| The metrics endpoint | Production: `metis_http_request_duration_seconds` has buckets on the 150ms and 500ms lines, plus the engine's backlog — the main database's and each environment's, labelled `environment` — and the connection pools. `metis_tenant_scope_reads_total` counts the reads of an organization's project list; against the request rate it is what each request pays for scoping. | Always, on its own port |
| `deploy/grafana/metis-slo.json` | Availability and budget left over 30 days, burn rate, latency against the objectives, backlog, pools. | Import once |
| `deploy/kubernetes/alerts.yaml` | Pages on the error budget's burn rate and on a backlog nobody is claiming. | With the rules loaded |

## Numbers so far

| Measured | Result | Against |
| :--- | :--- | :--- |
| Read p95, `tests/slo`, PostgreSQL 17 (2026-08-26) | 11.1ms | 150ms |
| Action p95, same run | 13.8ms | 500ms |
| 5xx over those runs | 0.000% | 0.1% |
| Process starts, PostgreSQL | 170,569 a minute | 10,000 |
| Read p95, `tests/loadtest` at 500,000 instances | under 5ms | 150ms |
| Completion p95, concurrent writes, 200 instances and 16 writers (2026-09-25) | 18.9–22.3ms | 500ms |
| The same at 2,000 and at 5,000 instances | 19.5ms and 30.6ms | 500ms |
| Start p95 in those runs | 16.7–37.0ms | 500ms |
| Throughput in those runs | 314–424 instances/s end to end, 1,069–1,393 completions/s | reported |
| Dashboard statistics p95, an organization with 10,000 projects against one with 4 (2026-09-26) | 26.3ms against 2.1ms, and 201ms on a loaded run; six reads of the project list per request | 150ms |

Throughput is reported rather than asserted: it is a property of the hardware,
and a threshold would either prove nothing or fail on a busy machine.

The concurrent writes were measured on an Apple M5 Pro (15 cores) with
PostgreSQL 17.11 on the same machine, at a load average around 7. The job
worker polled every 100ms (`METIS_JOB_POLL_INTERVAL`; the default is 2s), so
instances/s measures the engine rather than the poll interval. With other
suites sharing the CPU and the database (load average 20–40), the same test
measured a completion p95 of 218ms at 2,000 instances and a start p95 of 766ms
at 5,000. That was contention, not volume: rerun on a quiet machine, 5,000
instances gave the figures above. Take a number to compare from a quiet machine.

A deadlock or a serialization failure fails that test; it is not retried past.
The engine serializes the work on one instance with a row lock at READ
COMMITTED, and nothing retries a transaction, so either would reach the caller
as a 500 for work that was not done. None has been seen.

## Tenant scope at ten thousand projects

Every scoped repository call works out what the caller may see by reading the
ids of every project in the caller's organization, and scopes its query with
`project_id = ANY($n)` — storm binds the list as one `uuid[]` parameter, not as
a literal list. `TestTenantScopeAtScale` measures what that costs: an
organization with 10,000 projects against one with 4, both with 20,000
instances and 20,000 tasks over 4 busy projects, 200 requests per read per
organization, the two sampled in turn so drift lands on both.

Measured 2026-09-26 on an Apple M5 Pro with PostgreSQL 17.11 on the same
machine. The machine was shared, at a load average of 6–15, so the latencies
moved between runs; the reads and the allocation did not. The p95 is from the
quietest of three runs, with the range over the three in brackets:

| Read | Reads of the project list per request | Small p95 | Large p95 | Allocated per request, small → large |
| :--- | ---: | ---: | ---: | ---: |
| Dashboard statistics | 6 | 2.1ms (2.1–16.0) | 26.3ms (26.3–201.3) | 0.85 MB → 55 MB |
| Instances, paged | 5 | 3.4ms (3.4–7.7) | 35.6ms (35.6–142.0) | 0.25 MB → 46 MB |
| Task inbox | 1 | 6.4ms (5.3–46.8) | 15.4ms (12.9–114.1) | 0.18 MB → 10 MB |
| Tasks by assignee | 1 | 2.1ms (2.1–5.6) | 31.7ms (31.4–85.8) | 0.30 MB → 10 MB |
| Task by id | 1 | 0.32ms (0.24–0.55) | 6.0ms (5.2–9.0) | 0.05 MB → 9.9 MB |
| Instance by id | 1 | 0.28ms (0.28–3.6) | 5.9ms (5.9–46.5) | 0.05 MB → 9.9 MB |
| Definitions | 1 | 0.36ms (0.36–2.8) | 6.1ms (6.1–43.3) | 0.06 MB → 9.1 MB |
| Waiting by step | 1 | 8.2ms (8.2–62.6) | 14.0ms (14.0–121.4) | 0.04 MB → 9.1 MB |

Two costs, both paid on every scoped call:

- **The read.** The list is read through the store, which reads whole rows a
  thousand at a time: at 10,000 projects, ten statements and about 10 MB of
  rows to keep 16 bytes of each, about 4ms. A request reads it once per scoped
  call — six times for the statistics — so on the loaded run the statistics went
  past the 150ms read target.
- **The list in the query.** 10,000 ids are a 200 KB parameter on every scoped
  statement. Planned with its values, a 10,000-element array takes about 1.5ms
  to plan. Planned generically — which PostgreSQL switches a prepared statement
  to after five runs when it estimates that cheaper — `project_id = ANY($1)`
  becomes a filter that walks the array for every candidate row: the assignee
  count took 23.6ms on its generic plan against 4.8ms planned with its values.
  That is why tasks by assignee pays more than its one read.

**Read once per request** (`tenantscope.Request`). The tenant resolver gives
each request somewhere to keep its organization's project ids, and every scoped
call in the request reuses the first read. It is kept for that request only:
ended when the endpoint returns, forgotten when the request creates or deletes a
project, and never used for a context that names another organization — so
nothing is cached across requests. Three runs after it, at a load average of
9–12:

| Read | Reads per request | Large p95 before | Large p95 after | Allocated per request, large, before → after |
| :--- | ---: | ---: | ---: | ---: |
| Dashboard statistics | 6 → 1 | 26.3–201.3ms | 6.3–9.8ms | 55 MB → 9.6 MB |
| Instances, paged | 5 → 1 | 35.6–142.0ms | 6.2–6.6ms | 46 MB → 9.8 MB |
| Task inbox | 1 | 12.9–114.1ms | 17.2–20.6ms | 10 MB → 10 MB |
| Tasks by assignee | 1 | 31.4–85.8ms | 8.4–29.7ms | 10 MB → 10 MB |
| Task by id | 1 | 5.2–9.0ms | 4.0–5.3ms | 9.9 MB → 9.9 MB |
| Instance by id | 1 | 5.9–46.5ms | 4.5–5.2ms | 9.9 MB → 9.9 MB |
| Definitions | 1 | 6.1–43.3ms | 4.2–4.8ms | 9.1 MB → 9.1 MB |
| Waiting by step | 1 | 14.0–121.4ms | 10.9–15.1ms | 9.1 MB → 9.1 MB |

The small organization's reads are where they were, and its statistics
allocate 0.55 MB rather than 0.85 MB. The scope a request keeps costs two
allocations and 128 bytes per authenticated request, about 25ns; a scoped call
that reuses it costs 6ns and allocates nothing (Go benchmark).

## Decisions that were measured

The shape of these came from a measurement, and the measurement is the reason
not to change them back:

| Where | Measured | Chosen |
| :--- | :--- | :--- |
| Claiming due jobs (`pg/job.go`) | One query over both statuses took 18.8ms at 100,000 due; one per status, merged, 0.04ms | Two ranged queries |
| Retention sweeps (`db.DeleteInBatches`) | Picking rows by key planned a hash join over the whole table on every batch | Batches picked by ctid; 1,000,000 rows in about a second |
| The storm connection pool (`db.NewPool`) | Built through storm's constructor, whose parameter encoders the generated code assumes: `tests/bpmn` in 18.4s against 25.3s on the same machine | storm's constructor |
| Script conditions (`logic.RunSandboxed`) | `new Array(1e9).join('x')` ran 37.6s against a 200ms budget; goja cannot interrupt one native call | Abandoned after the budget and a grace period; 0.70s |
| The tenant scope (`pg/tenant.go`, `tenantscope.Request`) | Read once per scoped call: six reads a statistics request at 10,000 projects, 26–201ms p95 and 55 MB. A subquery on `projects.organization_id` instead of the list cannot be written — storm has no subquery predicate, its semi-joins go from parent to children, and its joins return projections rather than rows — and would be wrong where it could: an environment's instances and tasks are in the environment's database, and its projects only in the main one | Read once per request and kept for that request only; 6–10ms and 9.6 MB |

## Profiling

Off by default. With `METIS_PPROF_ENABLED=true` the process serves pprof on
loopback, `127.0.0.1:6060` unless `METIS_PPROF_ADDRESS` says otherwise, never on
the API port:

```bash
kubectl -n metis port-forward deploy/metis 6060:6060 &

go tool pprof -top  'http://localhost:6060/debug/pprof/profile?seconds=30'   # CPU
go tool pprof -top  http://localhost:6060/debug/pprof/heap                   # live heap
go tool pprof -top  http://localhost:6060/debug/pprof/allocs                 # allocation since start
go tool pprof -top  http://localhost:6060/debug/pprof/mutex                  # lock contention
go tool pprof -top  http://localhost:6060/debug/pprof/block                  # blocking
curl -s 'http://localhost:6060/debug/pprof/goroutine?debug=1' | head -50     # what is running
```

- **Profile under load.** A CPU profile of an idle server is a profile of the
  poll loops. Run it while `tests/loadtest` or real traffic is going.
- **Compare, do not eyeball.** Take a profile before a change and one after,
  then `go tool pprof -top -diff_base before.pb.gz after.pb.gz`.
- **Mutex and block profiles are sampled only while profiling is on**, at one
  contended lock in a hundred and blocking of 10µs or more. Before 2026-09-25
  nothing set a rate, so both were served and always empty.

Two Go benchmarks exist for paths that are hot enough to have regressed before:

```bash
go test -run '^$' -bench . -benchmem ./server/transports/https/ ./server/domains/services/impl/sqlconnector/
```

## Not measured yet

- **Memory per script.** goja has no heap limit, so nothing bounds or measures
  what a script allocates (`security-plan.md` P0.2(c)). What is bounded is how
  many run at once, with `METIS_SCRIPT_CONCURRENCY`.
