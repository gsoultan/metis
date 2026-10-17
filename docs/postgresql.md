# Running Metis on PostgreSQL

PostgreSQL is the supported production database. SQLite is the default so that a
first run works with no setup at all, and it is genuinely fine for evaluating —
but it is a single-writer file, and Metis caps its pool at one connection for
that reason. It does not become a server database by being pointed at more load.

MySQL and SQL Server are supported and tested; the settings below are written
for PostgreSQL because that is what the latency targets are measured against.

---

## Connecting

```
DATABASE_URL=host=db.internal user=metis password=… dbname=metis port=5432 sslmode=require
```

`DATABASE_URL` is a **PostgreSQL DSN**, not a file path. Pointing it at a file
fails at startup with a parse error rather than silently using SQLite, which is
deliberate.

Once the setup wizard has run, the connection lives encrypted in `config.yaml`
and that takes precedence. `ENCRYPTION_KEY` must be the key it was written with;
a mismatch refuses to start, and the message says so.

---

## Connection pool

Sized by Metis, not left to `database/sql`'s defaults. Those are *unlimited*
open connections and two idle, and both halves hurt: a burst opens more
connections than PostgreSQL's default `max_connections` of 100 and fails every
caller at once, while a steady load closes and reopens connections between
bursts, paying a TLS handshake per burst.

| Variable | Default | Notes |
| :-- | :-- | :-- |
| `METIS_DB_MAX_OPEN_CONNS` | `25` | The ceiling. Sized against the 128 in-flight request limit rather than guessed. |
| `METIS_DB_MAX_IDLE_CONNS` | tracks open | Holding fewer idle than you routinely use is what causes reconnect churn. |
| `METIS_DB_CONN_MAX_LIFETIME` | `30m` | Bounded so a failover or a rolling database restart is picked up without restarting Metis. |
| `METIS_DB_CONN_MAX_IDLE_TIME` | `5m` | |

**Two pools per process.** While the repositories move from GORM to storm, a
process holds a pool for each, and `METIS_DB_MAX_OPEN_CONNS` sizes both — so
one process opens up to twice the ceiling. Until 2026-09-25 the storm pool, which
carries most of the engine's queries, ignored the setting and took pgx's
default: the larger of 4 and the machine's CPU count, however large the node.
A connection string that sets `pool_max_conns` sizes the storm pool itself.
The idle and lifetime settings above apply to the GORM pool; the storm pool
checks its connections every 5 seconds, drops idle ones after 30 seconds and
replaces each after 30 minutes.

`metis_db_pool_connections{pool="storm",state="acquired"}` against
`metis_db_pool_max_connections` shows how close the storm pool is to its
ceiling, and `metis_db_pool_acquire_waits_total` how often a caller had to wait
for a connection. The GORM pool reports as `go_sql_*{db_name="gorm"}`.

**Sizing.** Start at 25. Raise it only if `pg_stat_activity` shows Metis is not
the thing saturating the database and requests are queueing on connections. The
ceiling, doubled for the two pools, must stay comfortably below
`max_connections` divided by the number of things connecting — remember
migrations, your backup job and any read replica tooling also hold connections.

More is not better. A pool far larger than the database can serve concurrently
moves the queue from your application, where you can see it, into the database,
where you cannot.

---

## Server settings

Set these on the database, not in Metis. Each one bounds a failure Metis cannot
bound from the outside.

```sql
-- A query that runs longer than this is not going to finish usefully. Without a
-- ceiling, one pathological query holds a connection until somebody notices.
ALTER ROLE metis SET statement_timeout = '30s';

-- The important one. A transaction left open holds its locks and its snapshot,
-- which blocks writers and stops autovacuum reclaiming anything. Metis takes
-- row locks on process instances, so an abandoned transaction is a process
-- nobody else can advance.
ALTER ROLE metis SET idle_in_transaction_session_timeout = '60s';

-- Bounds how long a statement waits for a lock rather than piling up behind it.
ALTER ROLE metis SET lock_timeout = '10s';
```

`statement_timeout` of 30s is deliberately generous: it is a backstop, not a
latency target. The read target is 150ms at p95, so anything near 30s is already
a bug.

**Do not set `statement_timeout` below a few seconds.** Migrations run through
the same role and a table rewrite on a large installation legitimately takes
longer than a request does. If you set it tight, override it for the migration
window.

---

## Extensions and version

No extensions are required. Metis uses ordinary tables, `SELECT … FOR UPDATE`
for job claiming, and JSON columns stored as text. PostgreSQL 14 and later are
fine; the latency targets are measured on 17.

---

## Backups

See [`recovery.md`](recovery.md), which has the procedure, the RPO and RTO
targets, and the record of the last rehearsal. Two things worth repeating here:

- **Take a backup immediately before deploying a release containing a
  migration.** Migrations are forward-only. A rollback is an image change; it
  does not undo a schema change, and it cannot, because dropping a column
  destroys data.
- `scripts/backup.sh` and `scripts/restore.sh` exist and have been rehearsed
  end to end. A backup procedure nobody has restored from is a hypothesis.

---

## What to watch

```sql
-- Connections, by state. Growth in idle-in-transaction is the one that hurts.
SELECT state, count(*) FROM pg_stat_activity
WHERE datname = current_database() GROUP BY state;

-- The longest-running transaction right now.
SELECT pid, now() - xact_start AS open_for, state, left(query, 80)
FROM pg_stat_activity
WHERE datname = current_database() AND xact_start IS NOT NULL
ORDER BY xact_start LIMIT 5;

-- Tables that have grown without being vacuumed.
SELECT relname, n_live_tup, n_dead_tup, last_autovacuum
FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 10;
```

The tables that grow without bound over an installation's life are
`audit_logs`, `variable_snapshots`, `service_calls` and finished `jobs`, with
`process_instances`, `tasks`, `incidents` and `notifications` growing with the
work itself. Start by measuring, not by deleting.

Three tables are cut back by the server itself, because their rows answer a
question only for a while. Every replica sweeps them at start-up and every ten
minutes after that, 5,000 rows per statement, on the main database and on each
open environment's:

| Table | Kept for | Why that long |
| :--- | :--- | :--- |
| `webhook_deliveries` | 48 hours | Longer than any sender's retry schedule, which is what the record de-duplicates. |
| `idempotency_records` | 15 minutes once answered; a day if never answered | Answers older than the TTL are reclaimed, not replayed. An unanswered claim may still belong to a running request, because the server sets no write deadline. |
| `shared_counters` | 5 minutes | The rate-limit windows are one minute long. |

Three more are cut back only when you say for how long to keep them. **All
are off by default**, and an upgrade removes nothing until one is set:

| Setting | Removes | Never removes |
| :--- | :--- | :--- |
| `METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS` | `variable_snapshots` captured more than that many days ago. An instance's variable history then starts at the period's beginning. | Snapshots inside the period, of any instance. |
| `METIS_RETENTION_COMPLETED_JOBS_DAYS` | `jobs` that will never run again — `completed`, or `failed` with no unresolved incident naming them — neither due nor changed for that many days. | A `pending` or `running` job, and a `failed` job an open incident names: resolving the incident is what runs it again. The incidents themselves are kept. |
| `METIS_RETENTION_FINISHED_INSTANCES_DAYS` | `process_instances` that `completed` or were `cancelled` and have not changed for that many days, with their `tasks`, `jobs`, `external_tasks`, `service_calls`, `incidents`, `event_subscriptions`, `compensatable_activities`, `variable_snapshots` and `notifications`. Main database only. | The audit trail. An instance any deviation was recorded or asked for on; one with an open incident; a `failed` or running one; a finished child of a running instance, and a finished parent of a running child; instances in an environment's own database; and every instance of a case an administrator placed a retention hold on, until it is released. A process given a period of its own is swept by that period instead. |

Each is a whole number of days from 1; anything else is ignored with a warning
and removes nothing. They run in the same sweep, on every database, 5,000 rows
a statement, and the server says at boot which are on.

**The audit trail is never removed by the server**, at any setting: it is the
record of who did what. Finished instances, and everything they own, go only
when `METIS_RETENTION_FINISHED_INSTANCES_DAYS` is set; keeping or archiving
them is a decision for whoever owns that record, so the server makes it only
when told to.

**A retention hold keeps a case whatever its age.** An administrator places
one on an instance with `POST /api/v1/instances/{id}/retention-hold` and a
reason, and releases it with `.../retention-hold/release` and a reason; both
go on the instance's audit trail. The sweep then keeps the instance, the
instance that started its chain of calls, and every instance started from
that one. Each batch of the sweep first takes `retention_holds` in `SHARE`
mode, so a hold being placed is either seen or waited for, while sweeps on
several replicas still run side by side. A project's holds are listed by
`GET /api/v1/projects/{id}/retention-holds`.

**A process can have a period of its own.** An administrator sets one with
`PUT /api/v1/projects/{id}/retention-periods/{key}`, as `{"days": n,
"reason": "..."}`, for every version of that process. The sweep keeps each
finished instance for its process's period, and for
`METIS_RETENTION_FINISHED_INSTANCES_DAYS` where its process has none. A
process's period applies even when that setting is unset, and then only the
processes given one are swept. `"days": null` returns a process to the
installation's setting. Each change is a row of `retention_periods` that is
never rewritten, so the history of a process's periods, who set each and why,
is at `.../retention-periods/{key}/history`. The sweep takes this table in
`SHARE` mode with the holds, for the same reason.

A failed sweep is logged as `A retention sweep failed` with the table and the
database. The table then keeps growing until a sweep succeeds.

---

## A production configuration, end to end

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: metis-secrets
  namespace: metis
type: Opaque
stringData:
  # Generate both. Anything guessable is refused at startup, and a JWT secret
  # that can be guessed is forgeable into an administrator's token.
  #   openssl rand -base64 48
  ENCRYPTION_KEY: "…"
  JWT_SECRET: "…"
  DATABASE_URL: "host=db.internal user=metis password=… dbname=metis port=5432 sslmode=require"
```

Changing `ENCRYPTION_KEY` on its own makes existing encrypted data — process
variables and connector credentials — unreadable. Rotate it instead: the old
key goes in `ENCRYPTION_KEY_PREVIOUS` for reading, and `metis --reseal` seals
everything again under the new one. The procedure is in
[`runbooks.md`](runbooks.md), "Rotating secrets".

The rest of the deployment is in [`../deploy/kubernetes/`](../deploy/kubernetes/),
including `GOMEMLIMIT`, the probes and the resource limits, each with a comment
saying what it prevents.
