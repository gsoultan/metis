# Runbooks

What to do when something is wrong, written for whoever is on call rather than
for whoever wrote it.

Each entry says how you find out, what to look at first, and what to do. Where a
query is given it is the query — not a description of one — so it can be pasted
at 3am.

Two conventions throughout:

- `psql` examples assume you are connected to the Metis database. On SQLite,
  substitute `sqlite3 <path>` and drop the schema qualifiers.
- **Read before you write.** Every `UPDATE` here is preceded by the `SELECT`
  that shows you what it will touch. A process instance is a durable business
  commitment; a careless update is somebody's approval going missing.

Related: [`recovery.md`](recovery.md) for backup, restore and the supported
topology. [`integration.md`](integration.md) for connector behaviour and retry
semantics. [`postgresql.md`](postgresql.md) for database configuration.

---

## Alerts

Every rule in `deploy/kubernetes/alerts.yaml` maps to an entry here.

| Alert | Means | Go to |
| :-- | :-- | :-- |
| `MetisDown` | Not being scraped for 2 minutes | [Metis is down](#metis-is-down) |
| `MetisMetricsMissing` | Nothing is exporting metrics | [Metis is down](#metis-is-down) |
| `MetisSchemaDrift` | A model has no migration | [A missing migration](#a-missing-migration) |
| `MetisErrorBudgetBurning` | 5xx at 14.4× the budget's rate, over 1h and 5m | [Errors above budget](#errors-above-the-budget) |
| `MetisErrorBudgetSlowBurn` | 5xx at 6× the budget's rate, over 6h and 30m | [Errors above budget](#errors-above-the-budget) |
| `MetisErrorBudgetExhausting` | 5xx averaging over the budget for 3 days | [Errors above budget](#errors-above-the-budget) |
| `MetisReadLatencyOverTarget` | Reads past 150ms p95 | [Slow](#everything-is-slow) |
| `MetisActionLatencyOverTarget` | Actions past 500ms p95 | [Slow](#everything-is-slow) |
| `MetisSaturated` | Near the in-flight ceiling | [Slow](#everything-is-slow) |
| `MetisNoTraffic` | No requests for 15 minutes | [Quiet](#it-has-gone-quiet) |
| `MetisJobsWaitingUnclaimed` | A due job has waited over 10 minutes | [Nobody is claiming](#jobs-are-due-and-nobody-is-claiming-them) |
| `MetisJobLeasesNotReclaimed` | Expired leases unclaimed for 15 minutes | [Stuck in running](#a-job-stuck-in-running) |
| `MetisIncidentsRising` | 10 more open incidents than 30 minutes ago | [Incidents piling up](#incidents-are-piling-up) |
| `MetisEngineStateUnreadable` | The backlog gauges cannot be read | [Backlog unreadable](#the-engines-backlog-cannot-be-read) |
| `MetisDatabasePoolSaturated` | Pool 90% in use and callers waiting | [Pool exhausted](#the-database-pool-is-exhausted) |
| `MetisStrictTenantScopeDenied` | The strict scope denied a call site | [`strict-tenant-scope.md`](strict-tenant-scope.md) |
| `MetisCanaryErrorsAboveStable` | The canary's 5xx over 1% and twice stable's | [A canary is failing](#a-canary-is-failing) |
| `MetisCanarySlowerThanStable` | The canary's read p95 over 150ms and twice stable's | [A canary is failing](#a-canary-is-failing) |

---

## Metis is down

**One replica is the supported topology**, so this is a full outage rather than
reduced capacity. See `recovery.md` §2.1 before considering scaling out as a
remedy — it is not one.

```bash
kubectl -n metis logs deploy/metis --tail=100
kubectl -n metis describe pod -l app.kubernetes.io/name=metis | sed -n '/Events/,$p'
```

If it is crash-looping, the message on the first line is the cause. The four
that account for almost all of them:

| First line says | Cause | Do |
| :-- | :-- | :-- |
| `Refusing to start. A weak …` | `ENCRYPTION_KEY` or `JWT_SECRET` too weak | Set a strong one. `METIS_ALLOW_WEAK_SECRETS=true` starts anyway and warns every boot; use it only to get an existing installation back up, never as a fix. |
| `could not read config.yaml` | The config file exists and is unreadable or has an unknown key | Fix the file. It deliberately refuses rather than falling back — the fallback used to create a *fresh empty database* and serve from it. |
| `could not decrypt the database connection string` | `ENCRYPTION_KEY` does not match the one used at setup | Restore the original key. The data is not lost; it is unreadable with the wrong key. |
| `failed to open db` | The database is unreachable | See [Database failover](#database-failover). |

If the pod is running but not ready, readiness is a database ping:

```bash
kubectl -n metis exec deploy/metis -- /usr/local/bin/metis --version   # image sanity
kubectl -n metis port-forward deploy/metis 8080:8080 &
curl -s localhost:8080/readyz; echo
```

`/healthz` answers without touching the database, `/readyz` does not. If
`/healthz` is fine and `/readyz` is not, the problem is the database.

---

## A missing migration

`MetisSchemaDrift` fires when a model declares a table or column the database
does not have. **The features behind it return 500 on every request**, while
`/readyz` stays green because readiness only pings the database. This is why the
alert exists: the failure is otherwise entirely silent.

Find what is missing — startup logs one line per item:

```bash
kubectl -n metis logs deploy/metis | grep "Schema drift"
```

**A restart will not fix it.** Migrations are numbered and forward-only; the
column is missing because no migration creates it. The fix is a release
containing that migration.

Until one ships, the affected feature is unavailable. If the feature is
load-bearing for you, roll back to the previous image — the schema is
forward-compatible, so an older binary runs against a newer schema.

To fail deploys on this rather than discovering it in production, set
`METIS_REFUSE_SCHEMA_DRIFT=true` in staging. It is off by default because an
operator restarting at 3am should not be blocked by a column the running code
may never read.

---

## A stuck process instance

"Stuck" is almost always one of three things, and they are distinguishable.

### First, ask what it is waiting for

```sql
SELECT id, status, created_at, updated_at
FROM process_instances WHERE id = '<instance-id>';

-- The steps it holds a token on right now.
SELECT node_id, status, created_at FROM tasks
WHERE instance_id = '<instance-id>' AND status <> 'completed';

-- Work the engine owes it.
SELECT id, node_id, type, status, retries, max_retries,
       next_run_at, locked_by, lock_expires, last_error
FROM jobs WHERE instance_id = '<instance-id>' ORDER BY created_at DESC LIMIT 20;
```

### It is waiting on a person

A row in `tasks` with status `unclaimed` or `claimed` is not stuck; it is
waiting, correctly. Check the task actually reaches somebody: a user task whose
assignee and candidate lists are all empty is offered to nobody.

```sql
SELECT node_id, assignee_id, candidate_users, candidate_groups
FROM tasks WHERE instance_id = '<instance-id>' AND status <> 'completed';
```

If it names a group nobody is in, add a member. The task then appears in their
inbox with no further action.

### It is waiting on a job that keeps failing

See [A poison job](#a-poison-job).

### It raised an incident

The engine refuses to guess. A gateway with no matching condition and no default
flow raises an incident and stops, deliberately — that is not a bug, it is the
alternative to sending somebody's approval down an arbitrary branch.

```bash
curl -sH "Authorization: Bearer $TOKEN" \
  "$METIS/api/v1/incidents/<instance-id>" | jq '.incidents[] | {node_id, message, created_at}'
```

The incident inbox in the interface shows the same thing with the cause in plain
words and a **Retry** button, which is the supported fix. Use it after correcting
whatever the message names. If the cause was the process model itself, deploy a
corrected version — running instances continue on the version they started on.

---

## A poison job

A job that fails, retries, and fails the same way every time. It is bounded:
`max_retries` is reached, the job moves to `failed`, and an incident is raised.
The danger is the window before that, where it occupies a worker slot.

```sql
-- Jobs burning retries right now.
SELECT id, instance_id, node_id, type, retries, max_retries, next_run_at, last_error
FROM jobs
WHERE status <> 'completed' AND retries > 0
ORDER BY retries DESC LIMIT 20;

-- Jobs that gave up.
SELECT id, instance_id, node_id, last_error, updated_at
FROM jobs WHERE status = 'failed' ORDER BY updated_at DESC LIMIT 20;
```

Group by `last_error` before treating any one of them as individual: a hundred
failed jobs with the same message is one broken downstream, not a hundred
problems.

**Do not delete the row.** The instance is waiting on it; deleting it leaves a
process that will never move and no record of why. Resolve the incident once the
cause is fixed, which replays the work.

If a downstream is down rather than broken, the circuit breaker has already
stopped the pool filling with calls to it — that is by design, and it recovers
on its own. See `integration.md`.

### A job stuck in `running`

```sql
SELECT id, instance_id, node_id, locked_by, lock_expires, updated_at
FROM jobs WHERE status = 'running' AND lock_expires < now();
```

Rows here are claimed by a worker that is gone. **They recover by themselves**:
the worker's poll offers a running job whose lease has expired alongside the
pending ones, and reclaiming it counts as an attempt. The job's work and its
status commit together, so a reclaimed job whose work had already committed
completes without doing it again.

Until 2026-09-25 this paragraph was not true. The poll asked for pending jobs
only, so these rows stayed running for good and the processes behind them hung
with no incident. An installation upgraded from before then may have some; the
first poll after the upgrade picks them up.

A job that keeps losing its worker — reclaimed three times, or as many as its
retries allow if that is more — is failed with an incident saying *the worker
running this step stopped before it finished*. Treat that as the job killing
the worker, not the other way round: look at what the step runs (a script, a
call that returns something enormous) and at the pod's last OOM kill.

If the list is long right after a deploy, that is the drain budget being
exceeded — raise `METIS_SHUTDOWN_DRAIN` (default 20s) so a rollout waits for
work it has already claimed.

If `lock_expires` is in the *future* and the pod is gone, wait for it. Five
minutes is the lease.

---

## An expired connector credential

Shows up as service tasks failing against one partner with a 401 or 403, and an
incident naming the connector.

```sql
SELECT ci.id, ci.name, c.key, count(j.id) AS failing
FROM connector_instances ci
JOIN connectors c ON c.id = ci.connector_id
LEFT JOIN jobs j ON j.status = 'failed' AND j.last_error LIKE '%' || ci.name || '%'
GROUP BY ci.id, ci.name, c.key ORDER BY failing DESC;
```

Fix it in the interface: **Connectors → the connection → the credential field →
Save**.

Two things to know:

- **The stored secret is never shown.** The field displays a placeholder;
  leaving it alone keeps what is stored, and typing over it replaces it. You
  cannot read the old value back out, by design — it is encrypted at rest and
  masked at the API.
- **Clearing the field clears the credential.** That is deliberate, so a leaked
  key can be removed, but it means an accidental clear-and-save is a real
  change.

Then resolve the incidents. The work replays with the new credential.

---

## Errors above the budget

The budget is 0.1% of responses as 5xx over 30 days, and three alerts watch how
fast it is going:

- **`MetisErrorBudgetBurning`** pages: 14.4 times the allowed rate over both the
  last hour and the last five minutes, which spends a month's budget in about two
  days.
- **`MetisErrorBudgetSlowBurn`** warns: 6 times the rate over six hours and
  thirty minutes — a month's budget in five days.
- **`MetisErrorBudgetExhausting`** is a ticket: the hourly ratio has averaged
  over the budget for three days.

The ratios are recorded as `metis:http_error_ratio:rate5m`, `rate30m`, `rate1h`
and `rate6h`, which is what the SLO dashboard (`deploy/grafana/`) reads.

```promql
sum by (route) (rate(metis_http_requests_total{status_class="5xx"}[5m]))
```

A single route dominating points at that feature. If the route is one whose
tables are missing, see [A missing migration](#a-missing-migration) — that is
the most common cause of a sudden, total 5xx rate on one route.

Note that a caller's own mistake is **not** counted here: a malformed identifier
answers 400 and an absent one 404, on purpose, so a client typo does not spend
the budget or page you.

---

## Live updates stopped arriving

The inbox, the designer's collaborators and the SDK sandbox listen on one
stream per browser tab, at `/api/v1/events`.

```promql
metis_http_event_streams_open
sum by (status_class) (rate(metis_http_requests_total{route="/api/v1/events"}[5m]))
```

Streams are not counted as requests in flight, and they do not use the API's
backpressure slots: until 2026-09-25 each open tab held one of the 128, so a
busy morning's worth of tabs stalled every other call. They have limits of
their own — 2048 per process, 16 per account — and past them a stream is
refused with **503** (the process is full) or **429** (that account is), with
`Retry-After`. The browser retries with a backoff.

- **429s for one account** is a script, or a tab reloading itself. Nobody else
  is affected.
- **503s, `metis_http_event_streams_open` at 2048** is a process holding as many
  tabs as it will. Add a replica, or find what is opening streams it does not
  close.
- **Nothing refused and still no updates** usually means a proxy in the way is
  buffering or cutting the stream. Every stream sends a keep-alive comment
  every 25 seconds, so a proxy idle timeout shorter than that is the first
  thing to check; response buffering the second.

---

## Everything is slow

Read latency past 150ms or actions past 500ms at p95.

```promql
histogram_quantile(0.95, sum by (le, route) (rate(metis_http_request_duration_seconds_bucket[5m])))
sum(metis_http_requests_in_flight)
```

In order of likelihood:

1. **The database.** Check connections against your ceiling — the pool defaults
   to 25 (`METIS_DB_MAX_OPEN_CONNS`). If the application is queueing on
   connections, requests wait before any query runs.
   ```sql
   SELECT count(*), state FROM pg_stat_activity WHERE datname = current_database() GROUP BY state;
   ```
   Long `idle in transaction` entries are the ones to worry about; see
   `postgresql.md`.
2. **A slow partner.** Service tasks have a 30s ceiling
   (`METIS_HTTP_TIMEOUT`), so one slow downstream holds worker slots without
   holding request threads. It shows in job throughput before it shows in
   latency.
3. **Saturation.** `MetisSaturated` means near the 128 in-flight ceiling, past
   which requests are refused with 503 and `Retry-After` rather than queued
   without bound. One replica is the supported topology, so the answer is
   usually a slow dependency rather than more replicas.

---

## It has gone quiet

`MetisNoTraffic`. Expected outside working hours, suspicious inside them.

This alert exists because the alarming failures here are the quiet ones. Two in
particular look exactly like an idle system:

- **A job worker that stopped claiming.** Check that jobs are moving:
  ```sql
  SELECT status, count(*) FROM jobs GROUP BY status;
  SELECT max(updated_at) FROM jobs WHERE status = 'completed';
  ```
  A `completed` timestamp that stopped advancing while `pending` grows is a
  worker that is not running.
- **A tenant scope answering every query with nothing.** If
  `METIS_FEATURE_STRICT_TENANT_SCOPE` was recently turned on and lists went
  empty rather than erroring, that is the failure mode it has — silence, not an
  error. See `strict-tenant-scope.md` and turn it back off.

---

## Jobs are due and nobody is claiming them

`MetisJobsWaitingUnclaimed`. A due job is claimed within one poll — two seconds
by default (`METIS_JOB_POLL_INTERVAL`) — so a job ten minutes past its time
means no worker is claiming. Timers are late and service tasks are not running,
and from outside it looks like a quiet system.

**Which database?** The engine's series are one set per database. An alert
labelled `environment` (the environment's id) and `environment_name` is about
that environment: its jobs are in its own database, its workers log under its
name, and the SQL in this section and the next runs against that database, not
the main one. An alert with neither label is about the main database.

```promql
metis_jobs_due
metis_jobs_oldest_due_age_seconds
metis_jobs_due{environment_name="staging"}   # one environment
metis_jobs_due{environment=""}               # the main database only
```

**Is it falling?** If `metis_jobs_due` is going down, the workers are claiming
and cannot keep up — after an outage, or when many timers come due at once. It
drains by itself at `METIS_JOB_WORKERS` (default 10) jobs at a time per replica.
Raise the workers only as far as the database pool: above it they queue on
connections instead of working.

**Is it flat or growing?** Then nothing is claiming:

1. **Are the workers running?** Each process logs `Job worker started` at boot,
   with its worker count and poll interval. A process started with
   `--reset-password` prints a password and exits without starting any.
   ```bash
   kubectl -n metis logs deploy/metis | grep -E "Job worker|could not read the pending jobs"
   ```
2. **Can they reach the database?** `could not read the pending jobs` in the
   log, or `MetisDatabasePoolSaturated` firing, puts the problem there — see
   [The database pool is exhausted](#the-database-pool-is-exhausted) and
   [Database failover](#database-failover).
3. **Are they all busy on something slow?** A step that takes minutes — a
   partner that answers slowly, a script at its time limit — holds a worker for
   that long. `SELECT node_id, count(*) FROM jobs WHERE status = 'running' GROUP
   BY 1 ORDER BY 2 DESC;` shows what they are doing.

---

## Incidents are piling up

`MetisIncidentsRising`: ten more incidents are open than half an hour ago. One
incident is a job that ran out of retries; ten more at once are almost always
one cause.

```sql
SELECT d.key AS process, i.node_id, left(i.error, 120) AS error, count(*)
FROM incidents i
LEFT JOIN process_definitions d ON d.id = i.definition_id
WHERE i.status = 'open'
GROUP BY 1, 2, 3
ORDER BY 4 DESC
LIMIT 20;
```

One row usually dominates. By what it says:

- **A partner answering 5xx or not at all** — the partner is down. The circuit
  breaker has already stopped the workers queueing on it; see `integration.md`.
- **401 or 403** — a credential expired. See
  [An expired connector credential](#an-expired-connector-credential).
- **A step that never failed before a deploy** — the new version broke it. See
  [Rolling back a release](#rolling-back-a-release).

**Fix the cause before resolving.** Resolving an incident
(`POST /api/v1/incidents/{id}/resolve`) replays the work; resolved while the
cause is still there, it fails again and opens another.

---

## The engine's backlog cannot be read

`MetisEngineStateUnreadable`. At each scrape the metrics endpoint counts the due
jobs, the expired leases and the open incidents, with a two-second budget. While
it cannot, `metis_engine_state_up` is 0, the backlog series are absent, and the
alerts on them cannot fire — which is why this one exists.

The log gives the reason: `Could not read the engine's backlog for the metrics
endpoint`. Usually the database is unreachable (see
[Database failover](#database-failover)); if it answers everything else, the
counts are taking longer than two seconds, which on the `jobs` table means it
needs vacuuming — see `postgresql.md`.

Each database is read on its own, at the same time, so one environment's
database being down marks only that environment's `metis_engine_state_up` 0.
When the alert names an environment and the log has no backlog line for it,
this replica could not start the environment at all — its database unreachable
or its port taken. That is logged once per reason, as `This environment could
not be started` with the environment's name, and tried again every 15 seconds:
fix the cause and it is served within one check.

---

## The database pool is exhausted

`MetisDatabasePoolSaturated`: nine in ten of a pool's connections are in use and
callers are waiting for one. This is the step before requests time out waiting,
while the database itself looks idle.

```promql
metis_db_pool_connections{state="acquired"}
metis_db_pool_max_connections
rate(metis_db_pool_acquire_waits_total[5m])
```

```sql
-- What the connections are doing, longest transaction first.
SELECT pid, now() - xact_start AS open_for, state, left(query, 80)
FROM pg_stat_activity
WHERE datname = current_database()
ORDER BY xact_start NULLS LAST
LIMIT 10;
```

- **`idle in transaction` near the top** is a transaction holding its
  connection and doing nothing. That is a bug in whatever opened it; the
  `open_for` column says how long it has been going on.
- **The same query, active, many times** is a query that got slow — often a
  plan that changed after a data or index change. `EXPLAIN (ANALYZE, BUFFERS)`
  it.
- **Nothing unusual, just many** is a pool too small for the load. Raise
  `METIS_DB_MAX_OPEN_CONNS`, remembering it sizes two pools per process and that
  PostgreSQL's `max_connections` is shared with everything else that connects.

---

## Out of memory

The pod restarts, and its last state says `OOMKilled`:

```bash
kubectl -n metis describe pod -l app.kubernetes.io/name=metis | grep -A4 "Last State"
```

Work in flight is not lost: a job whose worker died is reclaimed when its lease
runs out. What matters is finding what allocated, before it happens again.

In order of likelihood:

1. **A script.** The sandbox bounds a script's time and how many run at once
   (`METIS_SCRIPT_CONCURRENCY`), and it cannot bound a script's memory — goja
   has no heap limit (`security-plan.md` P0.2(c)). A script that ignored its
   time budget is abandoned and keeps running; the log says `script ignored its
   interrupt and was abandoned`, naming how long it was given. Find the step
   and fix the script.
2. **A very large value.** A service task response or a variable of many
   megabytes is held in memory while the step runs, and again while it is
   encrypted and written.
   ```sql
   SELECT id, octet_length(variables) AS bytes
   FROM process_instances ORDER BY 2 DESC LIMIT 10;
   ```
3. **Open streams.** Each browser tab holds one; a process accepts 2048.

Profile rather than guess. With `METIS_PPROF_ENABLED=true` the process serves
pprof on loopback (`127.0.0.1:6060`):

```bash
kubectl -n metis port-forward deploy/metis 6060:6060 &
go tool pprof -top http://localhost:6060/debug/pprof/heap
```

`GOMEMLIMIT` in `deploy/kubernetes/metis.yaml` (850MiB of a 1Gi limit) makes
the garbage collector work harder near the ceiling. If you raise the container
limit, keep `GOMEMLIMIT` at about 85% of it.

---

## Rotating secrets

- **`JWT_SECRET`** — set a new value and restart. Every session ends and
  everyone signs in again; no data is affected.
- **`ENCRYPTION_KEY`** — the old key stays installed for reading while every
  sealed value is sealed again under the new one:

  1. Generate a new key: `openssl rand -hex 32`.
  2. Put it where the current key lives, and the old key in
     `ENCRYPTION_KEY_PREVIOUS`:
     - configured by environment: `ENCRYPTION_KEY` = new key;
     - configured by `config.yaml`: its `encryption_key` = new key. The file
       wins over `ENCRYPTION_KEY`, so changing only the variable does nothing.
  3. Restart. Everything written from now on uses the new key, and data sealed
     under the old one still reads. The log warns on every start while
     `ENCRYPTION_KEY_PREVIOUS` is set.
  4. Reseal, with the same configuration as the server:
     ```bash
     kubectl -n metis exec deploy/metis -- /usr/local/bin/metis --reseal
     ```
     It reads every text, json, jsonb and bytea column of every table, in the
     main database and in each environment's, plus the connection string in
     `config.yaml`. Every sealed value it finds under the old key is sealed
     again under the new one. It is safe while the server runs: a row is
     rewritten only if it has not changed since it was read.
  5. `metis --reseal-check` exits 0 once nothing is left under the old key, and
     fails, saying how many values remain, until then. Run `--reseal` again if
     it fails. Then remove `ENCRYPTION_KEY_PREVIOUS` and restart.

  Values reported as **unreadable** open under neither key. The log names each
  column. In a column of sealed data they were sealed under a key this
  installation no longer has, and were unreadable before the rotation. In a
  column of names or text they are values that only begin like sealed ones.

  **Backups taken before the reseal are still sealed under the old key.** If
  the key is being retired because it leaked, those backups are exposed to
  whoever has it. Keep the old key for exactly as long as you keep those
  backups.

---
## Database failover

Metis holds a pooled connection with a bounded lifetime
(`METIS_DB_CONN_MAX_LIFETIME`, default 30 minutes) precisely so a failover is
picked up without restarting it.

1. **Do not restart Metis first.** Connections re-establish on their own. A
   restart during a failover means startup migrations racing a database that is
   still electing a primary.
2. Confirm the database is actually serving:
   ```bash
   psql "$DATABASE_URL" -c 'SELECT 1'
   ```
3. Watch readiness recover:
   ```bash
   kubectl -n metis get pod -l app.kubernetes.io/name=metis -w
   ```
4. If readiness does not recover within a few minutes and the database is
   healthy, *then* restart the pod.

**After any failover, check for lost work.** A commit that did not survive the
promotion is data loss, and the engine cannot tell the difference between a job
that never ran and one whose result was rolled back. Idempotency protects
against a repeated *outbound call*, not against a lost commit.

```sql
SELECT status, count(*) FROM jobs WHERE updated_at > now() - interval '15 minutes' GROUP BY status;
SELECT count(*) FROM process_instances WHERE updated_at > now() - interval '15 minutes';
```

If the replica lagged, see `recovery.md` for the restore procedure and the
RPO/RTO targets.

---

## Rolling back a release

The schema is forward-only. An older binary runs against a newer schema, so a
rollback is an image change:

```bash
kubectl -n metis set image deploy/metis metis=ghcr.io/gsoultan/metis:<previous>
kubectl -n metis rollout status deploy/metis
```

**What a rollback does not undo is a migration.** If the release you are rolling
back from added a column and backfilled it, the column stays. That is deliberate
— dropping it would destroy the data — and it is why `recovery.md` says to take
a backup immediately before deploying a release containing a migration.

The deployment uses a `Recreate` strategy, so there is a short gap rather than
two versions running at once by accident: a rolling update would run the old
pod against a schema the new one is in the middle of migrating. To run two
versions at once on purpose, and watch the new one before it replaces the old,
use a canary.

---

## Rolling out through a canary

A canary runs the next release beside the stable pods, on a share of the
requests, so a release that fails in production fails for that share first.
`deploy/kubernetes/canary.yaml` is the canary: `metis.yaml`'s pod with another
image and `track: canary`, and `tests/drift` holds it to that.

It is judged on what each pod measures for itself, against the stable track
over the same ten minutes: its 5xx (`MetisCanaryErrorsAboveStable`) and its read
latency (`MetisCanarySlowerThanStable`). The engine's backlog gauges read the
database, so they are the installation's and not a track's. A canary that
raises incidents or leaves jobs waiting shows in `MetisIncidentsRising` and
`MetisJobsWaitingUnclaimed`, unsplit, so watch those as well.

**Before you start:**

- **The canary runs the release's migrations.** It migrates at boot, against
  the database the stable pods are using, and removing it does not undo them.
  Take the backup the release asks for now, not before the full rollout. The
  stable pods then run against the newer schema, which works because migrations
  are forward-compatible ([Rolling back a release](#rolling-back-a-release)). A
  release whose notes say its schema is not safe for the previous version cannot
  be canaried: roll it out whole.
- **Prometheus must see the track.** The alerts compare series by a `track`
  label, copied from the pod label ([`deploy/kubernetes/README.md`](../deploy/kubernetes/README.md)
  says how). Check it before trusting their silence:

  ```promql
  count by (track) (rate(metis_http_requests_total[5m]))
  ```

  Two rows, `stable` and `canary`, once the canary is ready. One row with no
  `track` means the label is not being copied, and the canary alerts cannot fire.
- **The database has room for another pod.** Each pod opens up to twice
  `METIS_DB_MAX_OPEN_CONNS`, 50 by default ([`postgresql.md`](postgresql.md),
  "Connection pool"). One canary beside one stable pod is 100 at the ceiling,
  which is PostgreSQL's default `max_connections`:

  ```sql
  SHOW max_connections;
  SELECT count(*) FROM pg_stat_activity;
  ```

**The share** is the canary's share of the ready pods behind the Service. One
canary beside `metis.yaml`'s one stable pod takes half the requests, and about
half the background jobs, since every pod claims them. For a quarter, run three
stable pods while it lasts (`kubectl -n metis scale deploy/metis --replicas=3`),
once the connections above allow it; `metis.yaml` says what more than one
costs.

**Start it:**

```bash
sed -i.bak 's|metis:vX.Y.Z|metis:v0.4.0|' deploy/kubernetes/canary.yaml
kubectl -n metis apply -f deploy/kubernetes/canary.yaml
kubectl -n metis rollout status deploy/metis-canary
```

**Let it soak.** Each alert needs ten minutes over a ten-minute window before it
fires, so give the canary at least half an hour at normal traffic, and longer if
the release changes something that runs on a schedule, such as a timer. Nothing
fired, and both rows still there, is the signal to promote.

**Promote:** the stable Deployment first, then remove the canary.

```bash
kubectl -n metis set image deploy/metis metis=ghcr.io/gsoultan/metis:v0.4.0
kubectl -n metis rollout status deploy/metis
kubectl -n metis delete -f deploy/kubernetes/canary.yaml
```

In that order the canary serves while the stable Deployment recreates its pod,
so the release goes out without the `Recreate` gap. Scale the stable Deployment
back down if you raised it.

**If either canary alert fires,** stop instead:
[A canary is failing](#a-canary-is-failing).

---

## A canary is failing

`MetisCanaryErrorsAboveStable` or `MetisCanarySlowerThanStable` fired. Over ten
minutes the canary answered 5xx at more than twice the stable track's rate, and
over 1%, or its read p95 is more than twice stable's, and over the 150ms target.
Both tracks share the database and the traffic, and only the image differs, so
this is the release and not the load.

**Keep its logs, then stop it.** Removing the canary sends every request back to
the stable pods, and its logs go with it:

```bash
kubectl -n metis logs deploy/metis-canary --since=1h > canary.log
kubectl -n metis delete -f deploy/kubernetes/canary.yaml
```

Work the canary had claimed gets `METIS_SHUTDOWN_DRAIN` (20s) to finish. Past
that, a stable pod picks it up when its lease runs out
([A job stuck in `running`](#a-job-stuck-in-running)), so nothing in flight is
lost; it runs again on the stable release.

**What stopping does not undo is the release's migrations**: they ran when the
canary started, and the stable pods are already running against them. If the
stable track starts failing once the canary is gone, the migration is the likely
cause: see [Rolling back a release](#rolling-back-a-release) and the backup
taken before the canary started.

Then find what the release broke. The metrics outlive the pod, so compare the
tracks by route over the window it failed in:

```promql
sum by (track, route) (rate(metis_http_requests_total{status_class="5xx"}[10m]))
```

```promql
histogram_quantile(0.95,
  sum by (track, route, le) (rate(metis_http_request_duration_seconds_bucket{method="GET"}[10m])))
```

A route that fails or slows on `canary` alone is where to start reading
`canary.log`.
