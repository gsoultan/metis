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

## Migration 31: a task records which run of its step it is for

A step that runs once per person keeps a token for each of them. A task did not
say whose it was, so completing one from the inbox counted an approval and
retired nobody's token: after the last approval the process moved on, and then
never completed. Migration 31 adds `tasks.iteration_id`, and a completion now
retires the run its task was created for.

**The migration itself** adds one nullable column, which rewrites no rows, and
waits **at most two seconds** for the table. If `tasks` is held longer the
upgrade stops with

```
tasks was held for more than 2s by a long query or transaction; the upgrade
stopped rather than hold every inbox behind it, and will finish when started
again once that ends
```

and nothing has changed: let whatever holds the table finish, and start Metis
again.

**Approvals already under way** need nothing. A task created before the upgrade
records no run, and completing it retires the lowest-numbered run still waiting
on its step. A step part-way through — one approval given before the upgrade,
two after — finishes once, and the token the earlier approval left behind goes
with it.

**Processes this had already stranded are not repaired.** An instance whose
approvers had all answered before the upgrade is `active`, holds tokens on the
approval, and has nothing open. This finds them:

```sql
WITH live AS (
  SELECT i.id, i.created_at,
         CASE WHEN jsonb_typeof(i.tokens::jsonb) = 'array'
              THEN i.tokens::jsonb ELSE '[]'::jsonb END AS tokens
    FROM process_instances i
   WHERE i.status = 'active' AND i.deleted_at IS NULL
)
SELECT l.id, l.created_at
  FROM live l
 WHERE jsonb_array_length(l.tokens) > 0
   AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(l.tokens) t
                    WHERE coalesce(t->>'iteration_id', '') = '')
   AND NOT EXISTS (SELECT 1 FROM tasks k
                    WHERE k.instance_id = l.id AND k.deleted_at IS NULL
                      AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated'));
```

Every row is an instance holding only iteration tokens with no task behind any
of them. Whether each one's business was in fact finished is a decision, not a
repair: end the ones that were with a migration's *End the instance* action
(`cancel`, in `docs/process-change-in-flight.md`), which records who decided
and why.

**"Two of three" now withdraws the third.** A step whose completion condition
is met withdraws the approvals still open and tells their holders. Before the
upgrade those tasks were left open; one left open by an *earlier* early finish
is refused with 400 when somebody completes it — *this step has already
finished and the process has moved on* — and stays in their list. They are the
open tasks, recording no run, of a step the instance no longer counts but still
holds run tokens for:

```sql
WITH live AS (
  SELECT i.id,
         CASE WHEN jsonb_typeof(i.tokens::jsonb) = 'array'
              THEN i.tokens::jsonb ELSE '[]'::jsonb END AS tokens,
         CASE WHEN jsonb_typeof(i.multi_instance::jsonb) = 'object'
              THEN i.multi_instance::jsonb ELSE '{}'::jsonb END AS counting
    FROM process_instances i
   WHERE i.deleted_at IS NULL
)
SELECT k.id, k.name, k.assignee
  FROM tasks k JOIN live l ON l.id = k.instance_id
 WHERE k.deleted_at IS NULL AND k.iteration_id IS NULL
   AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated')
   AND NOT l.counting ? k.node_id
   AND EXISTS (SELECT 1 FROM jsonb_array_elements(l.tokens) t
                WHERE t->>'node_id' = k.node_id AND coalesce(t->>'iteration_id', '') <> '');
```

Set them `canceled` once you have looked at them.

**A step now also finishes when everybody asked has answered**, whatever its
condition says. A condition that the list could not satisfy used to hold the
step for ever; check any condition that was relying on that.

**Work waiting for workers is withdrawn with its step.** When a step ends early,
or an interrupting boundary event ends it, the external tasks still open for it
are deleted, with one `parked_work_withdrawn` entry in the instance's history.
A worker still holding one is told there is no such external task. Reports from
a worker that does not hold the task's lock, or whose lease has run out, now
answer 400 instead of 5xx, so a client that retried on 5xx will stop retrying
them.

**A process called by a step that already ended** no longer moves its parent
on. The parent's history records `called_process_finished_late`. The called
process is not ended when its step is, so its tasks stay open until somebody
completes them.

**Versions imported from BPMN before this release** keep a multi-instance
completion condition where nothing evaluates it, and go on running all-of-N —
changing a deployed version under its running instances is not something an
upgrade should do. Import the file again (or export and re-import the version)
to deploy one that honours it.

**Ad-hoc sub-processes** now withdraw the steps still running inside them when
their completion condition is met, at any depth, with the tasks, the work
waiting for workers, and the events those steps were waiting for. One that
should wait for them instead says `cancelRemainingInstances="false"` in its
BPMN file.

## Completing a task sets only what its form declares

Completing a task used to write every variable the completion carried into the
process, so whoever completed a step could also set business data beyond it:
the approver of a refund could change the amount being refunded, or name
somebody else as the approver. A completion now sets only the variables its
task's form declares, and is refused otherwise. No migration runs; what changes
is what a completion may set.

**What a form declares** is the `id` of each of its fields:

- the fields of the form built in the designer — the step's `form_definition`
  — hidden ones included, because the inbox sends a hidden field's value too;
- the fields of the stored form the step's form key names, when a form with
  that key is stored in its project. A form key that names a form kept outside
  Metis, such as an imported process's embedded form, declares nothing: Metis
  cannot read its fields.

A task with no form declares nothing, so it completes only with no variables.

**Who is affected:** integrations that complete tasks through the API — the Go
SDK's `CompleteTask`, REST, Connect or gRPC — with variables the task's form
does not have, including any variable at all on a task with no form. The inbox
is not: it sends exactly the fields of the form it shows, and completes a task
with no form with none. Such a completion is refused with a 400 that names what
was refused:

```
this task's form has no fields named amount, approved_by; a task can set only the variables its form declares
```

or, for a task with no form, *this task has no form to declare amount; a task
can set only the variables its form declares*. Nothing changes when one is
refused: the task stays open and no variable is set, not even the declared
ones sent with it. Over Connect, as with every refusal a service gives, the
reason is in the reply's `error` field. External tasks are not affected: a
worker completing one sets what it returns, as before.

**Find them.** Upgrade with `METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true` (below)
and let the log name them: the first completion of each step that sets a
variable its form does not declare is named once, with the variables it set —
names, never values; at most ten, and a count of the rest:

```
{"level":"warn","setting":"METIS_ALLOW_UNDECLARED_TASK_VARIABLES","project":"0199…","definition":"refund","node":"approve","variables":["amount","approved_by"],"message":"A completion of this step set variables its form does not declare, which only this setting allows. Give the step's form those fields, then turn the setting off."}
```

Each server remembers the steps it has named, so a restart, or another
replica, names a step again. This lists the open tasks by process, version and
step, and whether each has a form at all; one without a form can be completed
only with no variables:

```sql
SELECT d.key AS process, d.version, t.node_id AS step,
       (COALESCE(t.form_definition, '') NOT IN ('', '[]') OR COALESCE(t.form_key, '') <> '') AS has_form,
       count(*) AS open_tasks
  FROM tasks t
  JOIN process_instances i ON i.id = t.instance_id
  JOIN process_definitions d ON d.id = i.definition_id
 WHERE t.deleted_at IS NULL
   AND t.status IN ('unclaimed', 'claimed', 'delegated', 'escalated')
 GROUP BY 1, 2, 3, 4
 ORDER BY 1, 2, 3;
```

**Fix them** by giving each named step's form a field for each variable its
completions set — in the designer, or in the stored form its form key names —
and deploying. If a variable should not be the person's to set, leave the
field out and change the integration instead: that is the hole this closes. A
task keeps the form it was created with, so the tasks already waiting keep the
old one: running instances stay on the version they started on, and moving them
to the new version rebuilds a task only when its step's id changes.

**Need time?** `METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true` brings the old rule
back: a completion may set any variable, as before. It is for a migration
window, not a steady state, and the server says so at every boot while it is
on:

```
{"level":"warn","setting":"METIS_ALLOW_UNDECLARED_TASK_VARIABLES","message":"Completing a task can set any process variable, including ones its form does not declare, because this setting is on. The log names each step that does it, once; give those steps' forms the fields, then turn it off."}
```

Turn it off when three things hold: every step the log has named is fixed;
after restarting the servers once the last fix is deployed, the log names no
step for a full business cycle — the month-end run, the quarterly review; and
the query above shows no open task on a version from before its step was
fixed. A server names each step once, so a log gone quiet without a restart
may only mean it has named everything already.

**Rolling back** to the previous release brings the old rule back with no
setting; nothing in the database changed.

## Roles can be granted in one organization

Migration 30 gives every account's membership of an organization a list of
roles of its own (`user_organizations.roles`, empty to start). Nothing is moved
into it: every role an account held is still held on the account, and acts in
every organization the account belongs to, exactly as before. **Nothing changes
for anybody's access until somebody grants a role in an organization.**

What does change at once is who may change what:

- **A role held in every organization is the platform's to change.** Granting
  one or taking one away — in an account's dialog on Platform access, or
  `roles` on `PUT /api/v1/users/{id}` — creating an account that holds one, and
  deleting an account that holds one take a platform administrator: an
  administrator of every organization whose account id is listed in
  `METIS_PLATFORM_ADMINS`, on an installation of more than one organization,
  and any administrator of every organization on an installation of one. On an
  installation of several organizations, **set `METIS_PLATFORM_ADMINS` before
  anybody needs to do any of that**: until it is set nobody can, and an
  administrator who tries is told their account id to pass on.
- **The Roles tab grants in the organization being worked in.** A tick there
  used to change the account's own roles, and so its roles in every
  organization. It now changes what the account holds in this organization;
  a role held everywhere is shown as *Every organization*, with no box.
- **Adding an organization and managing the platform accounts** take the
  Administrator role held in every organization — which, before this, every
  administrator held. One granted in a single organization never reaches them,
  nor connector templates and manifests.
- **The last administrator of every organization is kept**, as the last
  administrator of an organization is. Only such an account can do the above,
  and no role held in one organization makes somebody one again, so keep at
  least one — and name them in `METIS_PLATFORM_ADMINS`.

Migration 30 waits **at most two seconds** for `user_organizations`: signing in
reads it, and PostgreSQL queues every reader behind an `ALTER TABLE` that is
waiting. Held longer, the upgrade stops with:

```
user_organizations was held for more than 2s by a long query or transaction;
the upgrade stopped rather than hold every sign-in behind it, and will finish
when started again once that ends
```

Nothing has changed at that point. End what holds the table — the query under
*Migration 28 can stop the upgrade* finds it, with `'user_organizations'` in
place of `'audit_logs'` — and start Metis again.

**Moving a role into organizations.** For an account that should hold a role in
some of its organizations rather than all of them:

1. Grant it in each organization it should hold it in: tick it on the Roles tab
   while working in that organization, or send
   `PUT /api/v1/users/{id}/organization-roles` with `X-Organization-ID` naming
   the organization.
2. Then a platform administrator takes the role held everywhere away: clear it
   under *Roles in every organization* in the account's dialog, or send
   `PUT /api/v1/users/{id}` with the roles the account keeps.

In that order the account never goes without the role where it needs it, and
the last-administrator checks refuse a step that would leave an organization,
or the installation, with nobody to administer it. Who holds what, everywhere
and in each organization:

```sql
SELECT u.username, u.roles AS everywhere, o.name AS organization, m.roles AS here
  FROM users u
  JOIN user_organizations m ON m.user_id = u.id
  JOIN organizations o ON o.id = m.organization_id
 WHERE u.deleted_at IS NULL AND o.deleted_at IS NULL
 ORDER BY u.username, o.name;
```

A change takes effect at the next request: the account a token names is read
again as soon as its roles change on the replica that changed them, and within
`METIS_AUTH_CACHE_TTL` (five seconds unless set) on any other. Tokens carry
nothing that outlives it.

**Rolling back** to the release before leaves the column in place, where that
release does not read it: a role granted in one organization stops acting
anywhere, and the roles held on accounts act as they always did.

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

## Tasks nobody was named for are the administrators' and operators'

A task with no assignee and no candidates used to be anybody's: anybody
signed in to its organization could claim it and complete it, with variables
of their own. It is now an administrator's or an operator's to take — to
claim, to complete, or to give to somebody — and nobody else's, whichever
kind of task it is. User tasks changed in 0.4.0. Manual tasks change in the
release after it: 0.4.0 left them open because the designer had no field to
name anybody for one, and a manual step now has the user step's *Who does
this* fields. No migration runs; what changes is who may act on these tasks.

**Who is affected:** installations with processes whose user or manual tasks
name nobody, and the members who took those tasks from the inbox's board.
Every manual step designed before this release names nobody, because there
was no way to name anybody: coming from 0.4.0, the manual tasks your members
confirm today are the administrators' and operators' after the upgrade,
unless the setting below is on. After the upgrade:

- A member claiming or completing such a task is refused with a 403: *this
  task has no assignee and no candidates, so only an administrator or an
  operator can take it; ask one of them to take it or to give it to
  somebody*. The board no longer offers them Claim on it, and says who can
  take it instead. Delegating or assigning one is refused the same way.
- Administrators and operators find these tasks under *Available to Claim*,
  and may claim them, complete them, or assign or delegate them to the
  person they should have gone to.
- Tasks with an assignee or candidates are unchanged, of either kind.

**Find them.** The designer warns about each user or manual step that names
nobody. The tasks already waiting on such a step, with their kind:

```sql
SELECT id, name, type, node_id, instance_id, created_at
FROM tasks
WHERE deleted_at IS NULL
  AND status = 'unclaimed'
  AND COALESCE(assignee, '') = ''
  AND COALESCE(candidate_users::text, '') IN ('', '[]', 'null')
  AND COALESCE(candidate_groups::text, '') IN ('', '[]', 'null')
ORDER BY created_at;
```

**Fix them** by giving each step an assignee, candidate users or candidate
groups in the designer, and deploying. Running instances stay on the version
they started on, so the tasks already waiting keep naming nobody: an
administrator or an operator takes each one, or assigns it to the person it
should go to.

**Need time?** `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true` brings the old rule
back, for user and manual tasks alike: anybody signed in may claim and
complete such a task, and *Available to Claim* offers it to everybody. It is
for a migration window, not a steady state, and the server says so at every
boot while it is on:

```
{"level":"warn","setting":"METIS_ALLOW_UNASSIGNED_TASK_CLAIMS","message":"Anybody signed in to an organization can claim and complete its tasks that have no assignee and no candidates, because this setting is on. Give those steps an assignee or candidates, then turn it off."}
```

Turn it off once the query above finds nothing that still needs a member to
take it. The board does not know the setting, so while it is on a member
claims such a task from *Available to Claim* rather than from the board.

**Rolling back** to 0.4.0 makes a manual task that names nobody anybody's
again, with no setting, and user tasks stay the administrators' and
operators'; nothing in the database changed. A manual step given people after
the upgrade keeps them: 0.4.0 holds a manual task to its assignee and
candidates as well, though its designer does not show them.

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

## Migration 28 can stop the upgrade when the audit table is busy

Migration 28 numbers audit entries as they are written, so an instance's history
reads in the order it happened. It adds a nullable column and gives it a
default, which rewrites no rows, so it needs `audit_logs` to itself only for a
catalog update. But while it *waits* for the table, PostgreSQL queues every
later audit write behind it, and every step of every running process writes
audit entries. A canary runs the release's migrations as it starts, beside the
stable pods ([Rolling out through a canary](runbooks.md#rolling-out-through-a-canary)),
so one long read — an export, a report, an anti-wraparound vacuum — would stop
the stable pods' engine for as long as it ran; with nothing else serving, the
upgrade would hang without saying why.

So it waits **at most two seconds**. If `audit_logs` is held longer, the upgrade
stops with:

```
audit_logs was held for more than 2s by a long query, transaction or vacuum;
the upgrade stopped rather than hold every audit write behind it, and will
finish when started again once that ends
```

Nothing has changed at that point: the migration's transaction rolled back.
Find what holds the table (`SELECT pid, state, query_start, query FROM
pg_stat_activity WHERE pid IN (SELECT pid FROM pg_locks WHERE relation =
'audit_logs'::regclass)`), let it finish or end it, and start Metis again. An
orchestrator restarting a failed pod does the retry for you.

## Decision cells see the rest of the case

No migration, but decisions can answer differently after the upgrade, and the
tables that will are ones you can find first.

A condition cell used to be tested against its own column's value and nothing
else. A cell that named anything — `> minimum`, `> credit_limit`,
`[low..high]` — compared with nothing: the line did not match, `!=` matched
every case, and a range failed the decision with "cannot compare a number with
a null". A cell now sees every variable the decision is evaluated with, as DMN
specifies, so each of those compares with what it names. A table that has them
decides as written from the upgrade on, where before it decided as if the named
value were missing.

A word on its own, with no operator, is still the word, with one exception: a
word that is the name of another column of the same table is now that column's
value. `manager` in a table with a `manager` column compares with that column;
write `"manager"` to mean the word.

To list the cells worth a look before upgrading:

```sql
SELECT d."key", d.version, cell
FROM decision_definitions d
CROSS JOIN LATERAL jsonb_array_elements(d.rules::jsonb) AS rule
CROSS JOIN LATERAL jsonb_array_elements_text(rule -> 'inputs') AS cell
WHERE d.deleted_at IS NULL
  AND (cell ~ '(>=|<=|!=|<|>|=)\s*[A-Za-z_]'
       OR cell ~ '[A-Za-z_]\w*\s*\.\.|\.\.\s*[A-Za-z_]'
       OR btrim(cell) IN (SELECT btrim(input ->> 'expression')
                          FROM jsonb_array_elements(d.inputs::jsonb) AS input))
ORDER BY d."key", d.version;
```

It covers every version not deleted, live or not, and errs toward listing too
much: a function call such as `> date("2026-01-01")` shows up and decides
exactly as before. Rolling the release back restores the old reading; nothing
is stored differently.

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
