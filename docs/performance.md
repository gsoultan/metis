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
| `cmd/metisload` | A running server, out of process, under 1,000 concurrent clients: each starts a request, finds its task, claims it, completes it and reads the instance list, then does it again. It reports throughput, the p50/p95/p99 of each call, what was shed, the server's resident memory, Go heap, CPU and connection-pool waits from its own metrics, and how much the database grew. See *A thousand concurrent clients* below. | On request, against a server that has been set up: `make load LOAD_ARGS="-password ... -dsn ..."` |
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
| Dashboard statistics p95, an organization with 10,000 projects against one with 4 (2026-09-26) | Before: 26.3ms against 2.1ms, and 201ms on a loaded run; six reads of the project list per request. After: 2.9–6.0ms against 1.7–3.8ms, one read | 150ms |
| Slowest read at 10,000 projects after (2026-09-26) | Tasks by assignee, 30.1–58.7ms on a generic plan (see *What is left*) | 150ms |

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

**The ids only, in one statement** (`projectsOf`). The one read left read whole
project rows through the store, a thousand at a time, to keep 16 bytes of each.
It reads `SELECT id FROM projects WHERE organization_id = $1 AND deleted_at IS
NULL` now: one statement and about 1ms at 10,000 projects. Three runs after
both changes, at a load average of about 10:

| Read | Small p95 | Large p95 | Large p95 before either | Allocated per request, large: before → once per request → ids only |
| :--- | ---: | ---: | ---: | ---: |
| Dashboard statistics | 1.7–3.8ms | 2.9–6.0ms | 26.3–201.3ms | 55 MB → 9.6 MB → 0.75 MB |
| Instances, paged | 2.2–4.2ms | 3.6–7.1ms | 35.6–142.0ms | 46 MB → 9.8 MB → 1.4 MB |
| Task inbox | 6.5–8.9ms | 13.9–18.5ms | 12.9–114.1ms | 10 MB → 10 MB → 2.0 MB |
| Tasks by assignee | 2.2–3.7ms | 30.1–58.7ms | 31.4–85.8ms | 10 MB → 10 MB → 2.1 MB |
| Task by id | 0.31–0.85ms | 1.8–4.9ms | 5.2–9.0ms | 9.9 MB → 9.9 MB → 1.5 MB |
| Instance by id | 0.35–0.52ms | 2.1–2.8ms | 5.9–46.5ms | 9.9 MB → 9.9 MB → 1.5 MB |
| Definitions | 0.21–0.45ms | 1.4–2.3ms | 6.1–43.3ms | 9.1 MB → 9.1 MB → 0.7 MB |
| Waiting by step | 7.1–16.3ms | 8.4–20.4ms | 14.0–121.4ms | 9.1 MB → 9.1 MB → 0.7 MB |

Every read is inside the 150ms target at 10,000 projects, and the reads that
name a project — the dashboard, the instance list, definitions — cost the large
organization about what they cost the small one.

**What is left is the list inside the query**, on the reads that span the whole
organization: the inbox pays 7–10ms more, and tasks by assignee 8–55ms more
depending on the plan. Planned with its values, the assignee count takes 4.9ms;
on its generic plan, which PostgreSQL picks for a prepared statement it has run
five times when it estimates that cheaper, it takes 21–26ms, because the plan
reads the assignee's rows by the assignee index and walks the 10,000 ids for
each. An index on `tasks (assignee, project_id)` was tried and the generic plan
does not use it. Two ways out, neither taken here: plan these statements with
their values every time (pgx's describe-and-execute mode, or `plan_cache_mode`),
which changes how every query on the pool is planned; or give the tables a
project owns an organization column, so the scope is one indexed equality with
no list at all — a schema change and a backfill, in the main database and in
every environment's.

## A thousand concurrent clients

`cmd/metisload` drives a real server over HTTP, so what it measures is what a
client sees: the interceptor chain, JSON, the connection pool and the job
worker running alongside. Each virtual user signs in as the same account, sends
its own `X-Forwarded-For` (the rate limit is per address, and a load generator
on one host is otherwise one very busy client), and retries a call the server
shed after its `Retry-After`, as the UI and the SDK do. Every user starts at
once; there is no ramp.

```bash
go run ./cmd/metisload -users 1000 -duration 60s -user admin -password admin \
    -dsn 'postgres://metis:metis@localhost:5432/metis?sslmode=disable'
```

`-think 2s` puts a pause between a user's calls, which is closer to people; the
default of none keeps every user's next call in flight at once, which is closer
to an integration. `-metrics` is where the server's metrics are read from
(`127.0.0.1:9464` by default), and `-dsn` is optional: without it storage is not
measured.

Measured 2026-10-23 on a 4 vCPU, 16 GB Linux VM, with PostgreSQL 16 and the
generator on the same machine, default settings throughout:

| Measured | 1,000 users | 100 users |
| :--- | :--- | :--- |
| Requests approved end to end | 128/s (640 API calls/s) | 135–157/s |
| Start, p50 / p95 | 390ms / 532ms | 79–86ms / 135–152ms |
| Complete, p50 / p95 | 371ms / 524ms | 85–91ms / 135–155ms |
| Instance list, p50 / p95 | 608ms / 859ms | 283–337ms / 360–441ms |
| Calls shed with 503 | 36,822, all retried | none |
| Server resident memory, peak | 170 MiB (Go heap 91 MiB, 1,500 goroutines) | 120–130 MiB |
| Server CPU | 1.2 cores; 9.6ms per approved request | 0.9 cores; 6.4–6.9ms |
| Database growth | 12.8 KiB per approved request | |

What that shows:

- **The Go server is not the limit.** It used about one core of four and under
  200 MiB at 1,000 concurrent clients. Under the same load PostgreSQL used
  about 2.4 cores, and the machine was at 100%.
- **More connections made it slower, not faster.** With the rate limit lifted
  and 100 users, a pool of 25, 50 and 100 gave 140, 129 and 121 approvals a
  second: the database was busy, and more connections only added contention.
  Keep `METIS_DB_MAX_OPEN_CONNS` near the in-flight limit.
- **The backpressure limit is the 1,000-client experience.** 128 running and
  256 waiting is 384 admitted, so the rest are refused with a 503 and retry a
  second later. `METIS_HTTP_MAX_QUEUED=1024` refused nothing in the same run
  and served the same throughput (106/s against 101/s), with a start p95 of
  4.8s instead of 0.8s. A deeper queue turns refusals into waiting; it does not
  make more room. The defaults stay where they are.
- **The instance list is the most expensive call, and it grows with the
  project.** It was about 90% of the database's time: the page was read as
  `project_id = ANY($1)`, which cannot use `ix_process_instances_project_created`
  in order, and every page also counts the project's instances twice, once in
  total and once by status. Both counts read every instance in the project, so
  the list slows as the project fills, and with the page fixed the two counts
  were still 100s of 116s of database time in a 30-second run at about 25,000
  instances. That store is being reworked, and this is the number to rerun.
- **Storage is about 10 KiB per finished three-step instance**, most of it the
  audit trail (eight rows, about 2.7 KiB), then variable snapshots (three rows),
  the task, the instance and two notifications. `broadcast_events` adds about
  1.5 KiB per live update while it is kept, and is pruned.

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
| The scope's read (`pg.projectsOf`) | Through the store: ten keyset statements of whole rows at 10,000 projects, about 4ms and 10 MB. The ids alone: one statement, about 1ms and 0.3 MB | Raw SQL for the ids, with the store's soft-delete predicate written out; statistics 2.9–6.0ms and 0.75 MB |

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
