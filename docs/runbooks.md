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
| `MetisErrorBudgetBurning` | 5xx above the budget | [Errors above budget](#errors-above-the-budget) |
| `MetisReadLatencyOverTarget` | Reads past 150ms p95 | [Slow](#everything-is-slow) |
| `MetisActionLatencyOverTarget` | Actions past 500ms p95 | [Slow](#everything-is-slow) |
| `MetisSaturated` | Near the in-flight ceiling | [Slow](#everything-is-slow) |
| `MetisNoTraffic` | No requests for 15 minutes | [Quiet](#it-has-gone-quiet) |

---

## Metis is down

**One replica is the supported topology**, so this is a full outage rather than
reduced capacity. See `recovery.md` §2.1 before considering scaling out as a
remedy — it is not one.

```bash
kubectl -n metis logs deploy/metis --tail=100
kubectl -n metis describe pod -l app=metis | sed -n '/Events/,$p'
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

`MetisErrorBudgetBurning` is 5xx responses past the 0.1% target.

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
   kubectl -n metis get pod -l app=metis -w
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
two versions running at once. That is intentional: one replica is the supported
topology, and a rolling update would briefly break it.
