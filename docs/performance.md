# Performance

How Metis is measured, the numbers it has shown, and how to profile it when a
number moves. The targets are the roadmap's §1 and they do not relax as data
grows: a read p95 under **150ms**, a workflow action p95 under **500ms**, under
**0.1%** of responses as 5xx, and **10,000** process starts a minute.

## Where each target is measured

| Where | What it proves | When it runs |
| :--- | :--- | :--- |
| `tests/slo` | The HTTP handler meets the targets in-process, with one to two orders of magnitude of headroom. It catches a regression that costs an order of magnitude, such as an N+1 or a per-request compile. | Every `make test` |
| `tests/loadtest` | The same targets hold with production-shaped volume across tenants. It catches a plan that flips at scale, which ten rows cannot show. | On request: `METIS_LOADTEST=1 go test ./tests/loadtest/ -v -timeout 30m`, sized with `METIS_LOADTEST_INSTANCES` and `METIS_LOADTEST_TENANTS` |
| `tests/loadtest`, concurrent writes | Many people completing work at once, on a process that splits into two approvals, joins, and calls a partner. Every instance finishes once, every task is completed once, nothing is left at the join, and the partner is called once per instance under its own key. A second submission of a completion is refused with a 400, and nothing else may fail. The action target is asserted; throughput is reported. | On request: `METIS_LOADTEST=1 go test ./tests/loadtest/ -run ConcurrentApprovals -v`, sized with `METIS_LOADTEST_WRITE_INSTANCES` (200) and `METIS_LOADTEST_WRITE_WORKERS` (16). `METIS_LOADTEST_SEED` replays an order of submissions |
| The metrics endpoint | Production: `metis_http_request_duration_seconds` has buckets on the 150ms and 500ms lines, plus the engine's backlog — the main database's and each environment's, labelled `environment` — and the connection pools. | Always, on its own port |
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

## Decisions that were measured

The shape of these came from a measurement, and the measurement is the reason
not to change them back:

| Where | Measured | Chosen |
| :--- | :--- | :--- |
| Claiming due jobs (`pg/job.go`) | One query over both statuses took 18.8ms at 100,000 due; one per status, merged, 0.04ms | Two ranged queries |
| Retention sweeps (`db.DeleteInBatches`) | Picking rows by key planned a hash join over the whole table on every batch | Batches picked by ctid; 1,000,000 rows in about a second |
| The storm connection pool (`db.NewPool`) | Built through storm's constructor, whose parameter encoders the generated code assumes: `tests/bpmn` in 18.4s against 25.3s on the same machine | storm's constructor |
| Script conditions (`logic.RunSandboxed`) | `new Array(1e9).join('x')` ran 37.6s against a 200ms budget; goja cannot interrupt one native call | Abandoned after the budget and a grace period; 0.70s |

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
