# Recovery: RTO, RPO and the procedures behind them

This closes the last unchecked item in [`../.junie/roadmap.md`](../.junie/roadmap.md) §1, and
it is the prerequisite for migration rollback — the migration runner only goes forward, so
recovering a bad migration means restoring a backup.

**Targets are a commitment, not an aspiration.** An RTO nobody has rehearsed is a guess. §4
is the rehearsal, and it is the part most likely to be skipped and most likely to be needed.

---

## 1. What is actually at risk

A process instance is a durable business commitment. That is what makes data loss here
different from losing a cache: an instance that vanishes was a purchase order somebody is
waiting on, a payment half-made, an approval someone believes they granted.

| State | Table(s) | Losing it means |
| :-- | :-- | :-- |
| **Running instances** | `process_instances`, `jobs`, `event_subscriptions` | Work in flight stops, silently. Nobody is told; the requester simply waits forever. |
| **Human tasks** | `tasks` | Approvals disappear from inboxes. A completed one may be re-requested. |
| **Definitions & decisions** | `process_definitions`, `decision_definitions`, `deployments` | Running instances pin a version; losing it strands them mid-flight. |
| **Audit trail** | `audit_logs`, `variable_snapshot` | The compliance answer to "who approved this". Usually the hardest loss to explain. |
| **Credentials** | `connector_instances` | Recoverable by re-entering them, *if* anyone still knows them. |
| **Identity** | `users`, `groups`, `memberships`, `organizations`, `projects` | Nobody can log in. |

Two things are **not** in the database and are lost independently:

- **`ENCRYPTION_KEY`.** Process and task variables are encrypted at rest. A database backup
  without this key restores rows that cannot be read. **Back it up separately, and never in
  the same store as the database backup** — one compromise should not yield both.
- **`config.yaml`**, which holds the connection string and JWT secret.

---

## 2. Targets

Per environment, because the cost of meeting them differs by an order of magnitude.

| Environment | RPO (data loss) | RTO (downtime) | How it is met |
| :-- | :-- | :-- | :-- |
| **Production** | **5 minutes** | **1 hour** | Point-in-time recovery: continuous WAL/binlog archiving plus a nightly base backup. |
| **Staging** | 24 hours | 4 hours | Nightly snapshot. |
| **Development** | No target | No target | Recreate by running setup. Do not spend money here. |

### Why these numbers

**RPO of 5 minutes in production** is not arbitrary. It is the interval a running instance
can lose without a human being able to tell the difference: an external call made in the
lost window may have already been delivered, so the instance retries it after recovery. Every
outbound connector call already carries an engine-generated idempotency key, so a retry is
safe — that mechanism is what makes a non-zero RPO tolerable at all. Push RPO much beyond
this and the retries stop being idempotent in practice, because the downstream system has
moved on.

**RTO of 1 hour** is chosen against how the product behaves while it is down: the engine is
not serving, so timers do not fire and SLAs silently slip. It is not chosen because an hour
is comfortable.

**These are targets for the data.** Availability during a *non*-destructive outage is a
different mechanism — run more than one replica, which the schema migration lock already
supports.

---

## 3. Procedures

### 3.1 Backup

PostgreSQL (the reference deployment):

```bash
# Base backup, nightly.
pg_basebackup -h "$PGHOST" -U "$PGUSER" -D /backup/base-$(date +%F) -Ft -z -P

# Continuous archiving — this is what buys the 5-minute RPO.
# In postgresql.conf:
#   wal_level = replica
#   archive_mode = on
#   archive_command = 'test ! -f /archive/%f && cp %p /archive/%f'
```

Alongside it, and **into a different store**:

```bash
# The encryption key. Without this the restored database is unreadable.
printf '%s' "$ENCRYPTION_KEY" | gpg --encrypt --recipient ops@example.com > /secrets/gobpm-encryption-key.gpg
```

MySQL: `mysqldump --single-transaction --routines` plus binlog archiving. `--single-transaction`
matters — without it the dump is not a consistent snapshot, and a process instance can be
captured in a state its own tokens contradict.

### 3.2 Restore

Order matters, and step 1 is the one that gets skipped.

1. **Stop the engine.** Every replica. A running engine against a half-restored database will
   claim jobs, run them, and write results the restore then overwrites — turning a clean
   recovery into a reconciliation problem.
2. Restore the base backup, then replay WAL to the chosen point in time.
3. Restore `ENCRYPTION_KEY` and `config.yaml`.
4. Start **one** replica. Watch the log for `Database migrations complete`. A restored
   database may be at an older schema version; the runner brings it forward.
5. Check `/readyz` returns 200.
6. Confirm the engine is working, not merely up — see §4.
7. Start the remaining replicas.

### 3.3 After a restore: what to expect

Recovery to a point in time means the engine resumes from that point, and the world did not.

- **Timers that should have fired during the lost window fire immediately** on restart, in a
  burst. This is correct, and it looks like a stampede.
- **External calls made after the recovery point are made again.** Idempotency keys make this
  safe for connectors that honour them. Connectors calling APIs that do not support
  idempotency may double-send; list those systems in advance, because after an incident is
  the wrong time to work out which they are.
- **Human tasks completed in the lost window come back**, and will be completed twice by
  someone who is certain they already did it once.
- **Audit entries in the lost window are gone.** If a compliance regime requires otherwise,
  the RPO above is too loose and must be renegotiated, not worked around.

---

## 4. Rehearsal

**A restore that has never been performed is a hypothesis.** Quarterly, against staging:

1. Note the time. Restore the most recent production backup into an isolated environment.
2. Start the engine. Measure to the first successful `/readyz` — that number is the real RTO,
   and it is usually larger than the estimate.
3. Verify the engine actually executes: start an instance from a known definition, complete a
   user task, confirm the token advances. A database that restores but cannot run a process
   has not been recovered.
4. Confirm encrypted variables are readable. If the key was not restored, this is where it is
   discovered — in a rehearsal, rather than during an incident.
5. Record the measured RTO/RPO against the targets in §2. If the measurement misses the
   target, change the mechanism or change the target. Leaving a target nobody meets is worse
   than having none, because it will be believed.

---

## 5. Migration rollback

The runner in `server/repositories/migrations` applies forward only, by design: a
down-migration for a change that dropped a column cannot invent the data back, and one that
pretends to is more dangerous than no rollback at all.

So a bad migration is recovered by restoring a backup. Which makes the procedure above the
rollback plan, and gives it one extra requirement:

> **Take a backup immediately before deploying a release that contains a migration, and
> confirm it completed before starting the deploy.**

`schema_migrations` records which version a database reached and when, so the recovery point
can be chosen precisely.
