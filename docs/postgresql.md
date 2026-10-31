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

**One pool once the server is up.** Every repository reads and writes through
the storm pool. GORM opens a second pool at boot to run the numbered
migrations, for the main database and for each environment, and closes it as
soon as they finish, so it never holds connections beside a serving process.
Until 2026-10-22 it stayed open for the life of the process, and the ceiling
below applied to each pool, so one process could open up to twice it.
Until 2026-09-25 the storm pool ignored the setting and took pgx's default:
the larger of 4 and the machine's CPU count, however large the node.
A connection string that sets `pool_max_conns` sizes the storm pool itself.
The idle and lifetime settings above apply to the migration pool; the storm
pool checks its connections every 5 seconds, drops idle ones after 30 seconds
and replaces each after 30 minutes.

`metis_db_pool_connections{pool="storm",state="acquired"}` against
`metis_db_pool_max_connections` shows how close the storm pool is to its
ceiling, and `metis_db_pool_acquire_waits_total` how often a caller had to wait
for a connection. The `go_sql_*{db_name="gorm"}` series are gone with the
pool they described.

**Sizing.** Start at 25. Raise it only if `pg_stat_activity` shows Metis is not
the thing saturating the database and requests are queueing on connections. The
ceiling must stay comfortably below
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

Four tables are cut back by the server itself, because their rows answer a
question only for a while. Every replica sweeps them at start-up and every ten
minutes after that, 5,000 rows per statement, on the main database and on each
open environment's:

| Table | Kept for | Why that long |
| :--- | :--- | :--- |
| `webhook_deliveries` | 48 hours | Longer than any sender's retry schedule, which is what the record de-duplicates. |
| `idempotency_records` | 15 minutes once answered; a day if never answered | Answers older than the TTL are reclaimed, not replayed. An unanswered claim may still belong to a running request, because the server sets no write deadline. |
| `shared_counters` | 5 minutes | The rate-limit windows are one minute long. |
| `external_task_workers` | 7 days since the worker last fetched | Long enough to say who served a topic before a weekend; worker ids made fresh at each start, as a container's hostname is, do not pile up. |

Finished instances are archived after 90 days unless you say otherwise, and
three more tables are cut back only when you say for how long to keep them:

| Setting | Removes | Never removes |
| :--- | :--- | :--- |
| `METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS` | `variable_snapshots` captured more than that many days ago. An instance's variable history then starts at the period's beginning. | Snapshots inside the period, of any instance. |
| `METIS_RETENTION_ARCHIVE_DAYS` | `archived_instances` archived more than that many days ago, for good. Main database only. | Unset, every archived instance. |
| `METIS_RETENTION_COMPLETED_JOBS_DAYS` | `jobs` that will never run again — `completed`, or `failed` with no unresolved incident naming them — neither due nor changed for that many days. | A `pending` or `running` job, and a `failed` job an open incident names: resolving the incident is what runs it again. The incidents themselves are kept. |
| `METIS_RETENTION_FINISHED_INSTANCES_DAYS` (unset: 90; `0`: never) | **Archives**, not removes: `process_instances` that `completed` or were `cancelled` and have not changed for that many days are written, with their `tasks`, `service_calls`, `incidents`, `notifications`, `variable_snapshots` and audit trail, to one sealed record in `archived_instances`; then those rows, their `jobs`, `external_tasks`, `event_subscriptions`, `compensatable_activities` and the audit entries naming nobody are removed. 100 instances a transaction. Every runtime archives its own: an environment's instances go to that environment's archive, read on its port. | The audit entries naming somebody who acted. An instance any deviation was recorded or asked for on; one with an open incident; a `failed` or running one; a finished child of a running instance, and a finished parent of a running child; and every instance of a case an administrator placed a retention hold on, until it is released. A process given a period of its own is swept by that period instead. |

Each is a whole number of days from 1, or `0` for never; anything else is
ignored with a warning and removes nothing. They run in the same sweep, on every database, 5,000 rows
a statement, and the server says at boot which are on.

**A finished instance is archived, not lost.** Its record keeps the
instance and everything it owned as they last stood, values and not the
sealed or offloaded forms they were stored in, so a record still opens after
its payload directory is gone. A project's archive is listed, newest first
and searchable by process and business key, by
`GET /api/v1/projects/{id}/archived-instances`, and one record is opened by
`GET /api/v1/archived-instances/{id}`, by whoever may read the project's
instances. The record column is sealed with `ENCRYPTION_KEY` as a whole, so
`metis --reseal` moves it with the rest. The audit entries naming somebody
who acted stay in `audit_logs` as well, so the audit log of who did what is
not cut short.

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
process's period applies even when that setting is `0`, and then only the
processes given one are archived. `"days": null` returns a process to the
installation's setting. Each change is a row of `retention_periods` that is
never rewritten, so the history of a process's periods, who set each and why,
is at `.../retention-periods/{key}/history`. The sweep takes this table in
`SHARE` mode with the holds, for the same reason.

A failed sweep is logged as `A retention sweep failed` with the table and the
database. The table then keeps growing until a sweep succeeds.

### Where archive records are kept

By default a record stays in its `archived_instances` row. Set
`METIS_ARCHIVE_STORE` to keep records elsewhere and the database smaller:

- **`directory`** with `METIS_ARCHIVE_DIR`: a file per record, under the
  project's id. Every replica must see the same directory, read-write. Back
  it up with the database.
- **`s3`** with `METIS_ARCHIVE_S3_BUCKET`: an object per record, under
  `METIS_ARCHIVE_S3_PREFIX`. `METIS_ARCHIVE_S3_ENDPOINT` and
  `METIS_ARCHIVE_S3_PATH_STYLE=true` reach an S3-compatible service.
  Credentials come from the AWS SDK's usual chain. Each call to the bucket
  has 30 seconds.

A record is moved after the transaction that archived it commits, by the
same sweep, so no call to a bucket or a file system is made while the
archive holds its locks. Until it moves it is in its row and opens from
there. Each moved record is sealed with a key of its own, and the row keeps
that key sealed with `ENCRYPTION_KEY` and the record's name: what is in the
store is unreadable without the database, and `metis --reseal` reseals the
rows without rewriting a single record. A record whose move failed half-way
is left behind in the store, sealed with a key nobody kept.

Changing `METIS_ARCHIVE_STORE` moves the records still in rows to the new
store on the next sweep; records already in another store stay there, and
open while that store's settings are still set. A store that is named and
cannot be opened stops the server at start-up.

### Large variable values

An instance's variables are one encrypted column, rewritten whole every time
the instance moves, and every move also copies them whole into
`variable_snapshots`. An instance carrying a 2 MB document therefore writes
2 MB of row and 2 MB of snapshot at every step, whether or not the document
changed, and each copy is kept.

Set `METIS_PAYLOAD_DIR` and a top-level variable larger than
`METIS_PAYLOAD_THRESHOLD_BYTES` (64 KiB by default) is kept in that directory
instead: sealed with `ENCRYPTION_KEY`, in a file of its instance's named by a
keyed fingerprint of its content, written once. The row and every snapshot
hold a reference of a few dozen bytes. A step that leaves the value as it was
writes the same reference and no file; a step that changes it writes one new
file. Everything that reads an instance gets the value, not the reference:
the engine, expressions, tasks, workers and the API see no difference.

The directory is laid out as `main/<instance-id>/` and
`environment-<id>/<instance-id>/`, one per database, and:

- **Every replica must see the same directory**, read-write: a shared volume
  or a network file system. The image's root is read-only, so mount it.
- **A value is kept for as long as its instance.** A file a later step
  replaced stays, because a snapshot may still refer to it; removing
  snapshots with `METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS` does not remove
  the files. The retention sweep removes an instance's directory once the
  instance is gone, and leaves any directory changed in the last hour.
- **A file that cannot be read is an error**, never a missing variable: the
  instance's reads fail until the file is back. Back the directory up with
  the database — `scripts/backup.sh` archives it when `METIS_PAYLOAD_DIR` is
  set, after the dump — and restore both.
- **`metis --reseal` reseals the files** with the columns when the key is
  rotated.
- Turning it on changes nothing already stored: a value moves out of its row
  the next time its instance is written. **Turning it off** keeps new values
  in their rows, but an instance that refers to the directory cannot be read
  without it, so leave it set while any does.

Only the instance row and its snapshots refer to the directory. The copies an
instance hands out — a task's or an external task's variables, a job's
payload, an audit entry — still carry the value itself.

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
