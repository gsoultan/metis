# Upgrading

## Rehearse it first

`scripts/upgrade-rehearsal.sh <backup-directory>` restores a backup into a
scratch database, boots this build against it, and checks the things a green
readiness probe does not: that no process instance was lost, that the columns
this release renames carried their data across, and that every account still
belongs to an organization.

It is read-only with respect to production — everything happens in a scratch
database, which is dropped afterwards unless you pass `--keep`.

Run it. The migrations that rename columns are only reachable *from* the old
schema, so a fresh install skips them entirely: they were written, reviewed, and
until this was added, never executed against a table that had the old names.
The first version of one of them silently left every form without its
definition. `tests/upgrade` is the automated version of the same rehearsal and
runs in CI; this is the one that uses your data.

## With OIDC on, local accounts sign in again

With `OIDC_ISSUER` and `OIDC_CLIENT_ID` set, the API used to take the identity
provider's ID tokens and nothing else, so a local account's token was refused
with 401 — including the administrator's you would need on the day the
provider is down. Both kinds are accepted now, each checked by its own rules;
[Signing in with OIDC](integration.md#signing-in-with-oidc) has the rule.

If you turned OIDC on in order to keep local accounts out, it no longer does:
every local account whose password works can sign in after the upgrade. Before
upgrading, list them, delete the ones nobody should use, and keep one
administrator with a strong password held offline:

```sql
SELECT username, roles FROM users
 WHERE identity_issuer IS NULL AND deleted_at IS NULL
 ORDER BY username;
```

## Migration 22 can stop the upgrade, on purpose

Seventy-two columns were declared non-null by the model — which is what the
generated reader is compiled from — and left nullable by `AutoMigrate`, which
does not carry that over. The reader decodes each of them by indexing a fixed
number of bytes, so a single NULL row is `index out of range`, not a zero
value, and **every read of that table answers 500**. That is what took the task
inbox down before it was found.

Migration 22 repairs them. For counters and versions it fills the gap with zero
— no retries yet, no attempts yet, version zero are all true statements about a
row that does not say. For **timestamps it refuses**:

```
webhook_deliveries.received_at holds 4 NULL(s), and every read of
webhook_deliveries panics on them.

This migration will not guess a timestamp: webhook_deliveries.received_at is
part of the record of when things happened, and an invented one is worse than a
stopped upgrade. Decide what those rows should say, set it, and run the upgrade
again. If they are junk, delete them
```

**You will almost certainly never see this.** The engine writes every timestamp
on every insert, so the only rows that can carry a NULL are ones written around
the application — a bulk import, a migration from another system, a fix applied
by hand. If it does fire, the migration has changed nothing: the column is still
nullable and the upgrade can be run again once the rows are decided. Set them,
or delete them if they are junk.

The repair takes no meaningful lock. `SET NOT NULL` on its own holds
`ACCESS EXCLUSIVE` while it scans the table, which stops every read and write
on it; this adds a `NOT VALID` check first and validates that under
`SHARE UPDATE EXCLUSIVE`, which readers and writers do not contend with, so the
exclusive lock is held for a catalog update rather than a scan.

## Migration 26: decisions have a live version

Saving a decision now adds a version instead of rewriting the one you opened,
and a step that names no decision version reads the decision's *live* version —
the one somebody made live — rather than its newest. Migration 26 records, for
every decision already stored, the version that was evaluating before the
upgrade (its highest version not deleted) as live, so nothing a process decides
changes when you upgrade. It runs in one transaction, and a second run changes
nothing.

A decision with no live version refuses to be evaluated without a version rather
than guess, so `decision_releases` matters as much as `decision_definitions`:
restore both, or neither.
## Migration 25: webhooks have ninety days to move to v2 signatures

A webhook signature used to cover the request body alone. A delivery captured
anywhere between a sender and Metis — a proxy log, a TLS-terminating load
balancer, the sender's own request logging — could be posted again under a new
delivery ID, and every copy was acted on. Deliveries are now signed with v2,
which covers the time of sending and the delivery ID as well; *Receiving
events: webhooks* in [`integration.md`](integration.md) has the scheme and
examples.

Migration 25 adds `webhooks.legacy_signatures_until` and sets it to **ninety
days from the upgrade** on every webhook that exists. Until then each one
accepts the old signature as before; after it, a delivery signed the old way is
refused with a message saying how to sign with v2. Webhooks created after the
upgrade accept v2 only.

- **Move your senders before the date.** The webhooks screen (Connectors,
  *Incoming webhooks*) shows it under each webhook still accepting the old
  signature, and the server logs each one still in use: *Accepted a webhook
  delivery signed the legacy way*, with the webhook's name.
- **Close a window early** once its sender has moved, because until it closes
  the old signature stays replayable. On the webhooks screen, *How to move the
  sender to v2* ends with **Stop accepting legacy signatures now**; the API is
  `DELETE /api/v1/webhooks/{id}/legacy-signatures`, for a designer. It only
  ever shortens a window.
- **Extend one** for a sender that cannot make the date, knowing its deliveries
  stay replayable meanwhile. That is deliberately not in the product: it is
  the operator's call, in SQL. The token is the last part of the address the
  screen copies:

  ```sql
  UPDATE webhooks SET legacy_signatures_until = now() + interval '30 days' WHERE token = '<token>';
  ```
- A webhook created by a replica still running the previous release, after the
  migration has run, gets no window: that release does not know the column. Its
  sender is told how to sign with v2 on the first refusal; give it a window with
  the statement above if it needs one.

**Rolling back** is safe for the data: the previous release ignores the column
and accepts the old signature from every webhook again, with no end date, as it
always did. It does not know v2, though, so a sender that already signs *only*
with v2 is refused until you upgrade again. Upgrading again does not re-run
migration 25, so every window keeps its original date.

## Moving to PostgreSQL

Metis runs on PostgreSQL and nothing else. SQLite, MySQL and SQL Server were
supported and are not any more.

An installation on one of them will not start. That is deliberate: the previous
behaviour for a driver the build did not recognise was to fall back to a local
SQLite file, which meant coming up healthy and empty — every process, task and
definition apparently gone, with a successful startup log and a readiness probe
that passed. Refusing to start says what happened.

To move:

1. **Back up first**, including the encryption key. `scripts/backup.sh` writes
   both, separately. A database backup without `ENCRYPTION_KEY` restores rows
   nothing can read.
2. **Stop the engine.** Migrating a database that is being written to gives you
   a copy of a moment that never existed.
3. **Move the data.** There is no built-in converter — the schemas differ in the
   column types each engine has a word for, which is the reason for the move.
   `pgloader` handles MySQL and SQLite; for SQL Server, dump and load.
4. **Point at the new database.** Either set `DATABASE_URL`, or edit
   `config.yaml` so `database.driver` reads `postgres` and re-encrypt the
   connection string with the same key.
5. **Start, and read the first hundred lines.** Schema drift between what is in
   the database and what this build expects is reported at startup rather than
   altered.

Why one engine: the storage layer is compiled rather than assembled at run time,
which is what lets a query's shape be checked before it runs and a soft-delete
predicate be a property of the schema rather than a rule every call site
remembers. That compiler emits PostgreSQL. Four dialects also meant four
spellings of every constraint, three of them exercised by a suite that skipped
unless somebody had a server running — so "the tests pass" routinely meant
"SQLite passes", and SQL Server once shipped declaring a column type it has no
word for.


## GoBPM is now Metis

The project, its module path and its repository are renamed. **An existing
installation keeps working without being reconfigured** — every old name is
still read — but each fallback is a migration aid with an expiry, not a second
supported spelling. This page is the list of things to change and when they stop
working.

### What must change now

Nothing, to keep running. One thing, to keep building:

```go
// go.mod, and every import
github.com/gsoultan/gobpm      →  github.com/gsoultan/metis
github.com/gsoultan/gobpm/sdk  →  github.com/gsoultan/metis-sdk
```

The Go client's package name changed with it — `gobpm.NewClient` is now
`metis.NewClient`. GitHub redirects the old repository URL, so `git remote` and
`go get` keep resolving, but the import path in your source has to be edited.

### The Go SDK is its own repository

The client was always its own module; it is now published from its own
repository, [gsoultan/metis-sdk](https://github.com/gsoultan/metis-sdk), so it
versions independently of the engine it talks to. If you already moved to the
`metis` spelling, this is the one further edit:

```go
github.com/gsoultan/metis/sdk  →  github.com/gsoultan/metis-sdk
```

Nothing else changes. The package name is still `metis` and every exported
symbol is identical, so only the import line moves. Unlike the environment
variables below, this one has no fallback — a nested module path cannot redirect
— so it is an edit to make now rather than one with an expiry.

### What still works, and for how long

| Was | Is | Until |
| :-- | :-- | :-- |
| `GOBPM_*` environment variables | `METIS_*` | Read, with a warning naming the variable. Removed in a future release. |
| `gobpm.db` (SQLite default) | `metis.db` | Opened if present. Kept indefinitely; renaming the file is optional. |
| `gobpm-*-storage` (browser) | `metis-*-storage` | Migrated on first load, automatically and once. |

**Environment variables.** Nothing is required of you. The server reads the old
name, uses it, and logs once per variable saying what to rename it to:

```
This setting is read under its old name. GoBPM is now Metis; the GOBPM_
spelling still works and will be removed in a future release.
  using=GOBPM_HTTP_ADDRESS  rename_to=METIS_HTTP_ADDRESS
```

`ENCRYPTION_KEY`, `JWT_SECRET` and `DATABASE_URL` were never prefixed and are
unchanged.

The reason for the fallback rather than a clean break: several of these change
behaviour when they go missing, and they do it quietly. An installation with
`GOBPM_FEATURE_JAVASCRIPT_CONDITIONS=true` would have come back up with the flag
at its default, and every gateway routing on a `js:` condition would have
stopped — with nothing in the log connecting that to an upgrade.

**The SQLite file.** A fresh install creates `metis.db`. An existing `gobpm.db`
is opened as it is, and the startup log says so. Rename it when convenient, or
point `DATABASE_URL` at it explicitly. If both exist, `metis.db` wins.

**Browser storage.** Nobody is signed out. The four `gobpm-`prefixed keys are
copied to their `metis-` names on first load and the old ones removed. Selected
project, theme and sidebar state come across with the session.

### What changed with no fallback

- The container entrypoint and binary are `metis`, not `gobpm`. A deployment
  that names the binary in a command override needs editing.
- `docker-compose.yml` uses `metis` for its evaluation database's user, password
  and database name, and a `metis-postgres` volume. **An existing evaluation
  stack starts empty** — it is throwaway by design, but if you were relying on
  it, rename the volume or point the compose file back at the old credentials.

- **Metrics and traces are renamed, and nothing forwards the old names.** The
  Prometheus metrics are `metis_http_requests_total`,
  `metis_http_request_duration_seconds` and `metis_http_requests_in_flight`; the
  OpenTelemetry service is `metis`, its span is `metis.http`, and its attributes
  are `metis.instance.id`, `metis.node.id`, `metis.definition.id`,
  `metis.connector.key` and `metis.attempt`.

  **A dashboard or alert rule written against the `gobpm_` names goes blank
  rather than failing.** A blank panel is easy to notice; a paging alert whose
  query matches nothing simply stops firing, which is not. Grep your alerting
  rules for `gobpm_` before upgrading, not after.

- Webhook deliveries send `User-Agent: Metis-Webhook/1.0`. Only relevant if a
  receiver matches on it.

### What deliberately did not change

The `Idempotency-Key` Metis sends on outbound service calls still begins with
`gobpm-`, and that is not an oversight.

The key is derived fresh on every retry rather than read back from storage, so
it is the only thing that identifies a retry to the receiving system as the same
request it already saw. Renaming it would mean a job that happened to be
between retries during the upgrade arrives looking new — and a service task
exists to have an effect out in the world, so "new" means charging the card a
second time. `TestTheServiceCallKeyIsFrozenAcrossTheRename` fails if anyone
finishes the job.

### Checking

The server tells you what it is reading. Start it and look for any line
mentioning an old name; when there are none, the fallbacks are doing nothing and
you can stop thinking about this page.
