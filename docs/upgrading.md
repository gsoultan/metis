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

## A second administrator approves waivers and skips (migration 34)

Some things one administrator could do to running work alone now wait for a
second administrator of the same organization. The upgrade runs migration 34
to make the table the requests are kept in. What the control is, and what it
does not cover, is in
[A second administrator](process-change-in-flight.md#a-second-administrator);
what an approver does is in
[the runbooks](runbooks.md#approving-a-request-for-a-second-administrator);
the routes are in
[Integrating with Metis](integration.md#requests-for-a-second-administrator).
This is what the upgrade does and what it changes for you.

**What now waits, and what does not.**

- **A waive of a step of one instance**
  (`POST /api/v1/instances/{id}/deviations`, `kind: waive`, `dry_run: false`)
  is no longer applied by the request that asks for it. The answer is **202**
  with `applied: false` and `pending_approval`; nothing about the instance
  has changed. The waive is made when a different administrator approves the
  request.
- **A migration** (`POST /api/v1/definitions/versions/migrate`,
  `dry_run: false`) waits the same way, with a **202**, when its plan says
  `requires_second_approver: true`. That is when, over at least one instance
  that has not ended, it does one of these:
  - skips a step (`node_actions` with `kind: skip`);
  - takes a step marked `compliance_relevant` from an instance that has not
    passed it, and the loss was acknowledged;
  - sends a step's work to a different step, while some instance has not
    passed some step marked `compliance_relevant`;
  - moves instances onto a version that takes away part of a step's
    `separation_of_duties` rule, while some instance has still to pass that
    step.
- **Unchanged, one administrator's call, answered 200:** a cancel or a hold of
  one instance in place; a migration's `cancel` and `hold`; a migration that
  only moves work and does none of the four things above; any migration of a
  version nothing unfinished runs on.

A script that waived a step, or migrated with a `skip`, in one call must now
do three things: read the 202, hand the request's id to a second
administrator, and have that account approve it
(`POST /api/v1/deviation-requests/{id}/approve`). A client that treats only
200 as success will take the 202 for a failure. One that reads `applied` is
already right: it is `false`.

**Three migrations that applied on one call and now ask, which you may not
expect.** Each is the direction that asks too often rather than too seldom.

- *Any redirect in a process with a control somebody has not passed.* The
  planner does not work out whether the control is still ahead of where the
  instance lands. It asks whenever there is anything to lose.
- *A step given a new id whose neighbours also changed.* A mapping onto a new
  id asks nobody only when the step is renamed where it stands: its old id is
  gone from the new version, and the same steps lead to it and follow it. A
  step renamed and moved, a renamed boundary event, a renamed event
  sub-process and a renamed step with no sequence flow all count as
  redirected. To rename a step without anybody being asked, give it a new id
  and change nothing else about where it stands.
- *A version whose only instances are suspended.* A suspended instance has
  not ended, so it is counted.

**A mapping that sends a control onto a different step is now a hold.** A step
marked `compliance_relevant` mapped onto another step used to count as carried
across whenever the step it landed on was marked too. It is now a control not
carried across, whatever the landing step is marked as: the plan lists it in
`compliance_holds` and refuses the apply until its id is in `acknowledge`.
Acknowledged, it waits for a second administrator. The same holds for a
control mapped onto a new id that does not stand where the old step stood.

**Finished work follows fewer mappings.** A finished task, and the instance's
list of completed steps, take a step's new id only under a rename. Two
mappings that counted as renames no longer do:

- a mapping of a step the new version still has under its old id;
- a rename onto a step marked `compliance_relevant` that does not stand where
  the old step stood.

Under either, finished work stays under the old id and the plan warns of it as
of any redirect. Before, such a mapping recorded the old step's finished work
as the control performed. The cost: a control that really was renamed while
its neighbours changed is not carried for the instances that passed it, so a
later migration that drops it holds for them too.

**What migration 34 does.** Six steps, each safe to repeat, so a run that
stops part-way finishes when the server is started again
(`TestMigration34KeepsTheOldVisitIndexUntilTheNewOneIsBuiltAndFinishesWhenStartedAgain`):

1. It creates `deviation_requests` and its indexes.
2. It adds `instance_deviations.live_visit_key`, and a reference from
   `instance_deviations.request_id` to the new table, not yet checked.
3. It gives every ledger row that holds its visit (`applied` or
   `pending_approval`) its live key, in batches of 5,000.
4. It builds three indexes on the ledger without locking it against writes.
5. It drops migration 33's unique index on the visit key, and only now: until
   the new unique index is valid, the old one is what refuses a second live
   row for a visit.
6. It checks the reference against every row.

The ledger's rule of one live row per visit moves from the visit key onto the
live key, which a row holds only while it is `applied` or `pending_approval`.
Under the old index a waive somebody rejected could never be asked for again.
PostgreSQL only, as migrations 21 to 33 are.

**It can stop the upgrade, on purpose.** Each step waits two seconds for a
lock and then stops, with one of five sentences followed by the database's
own error. For the first four, start the server again once the long query or
transaction has ended; nothing it had done is undone.

```
projects or process_definitions was held for more than 2s by a long query or
transaction; the upgrade stopped rather than hold every writer of either
behind it, and will finish when started again once that ends
```

```
instance_deviations was held for more than 2s by a long query or
transaction; the upgrade stopped rather than hold every hand-over and every
completion behind it, and will finish when started again once that ends
```

```
a row of instance_deviations was held for more than 2s by a long
transaction; the upgrade stopped rather than wait on it with row locks held,
and will finish when started again once that ends
```

```
instance_deviations was held for more than 2s by another change to its
schema or a vacuum; the upgrade stopped rather than wait behind it, and will
finish when started again once that ends
```

The fifth is not a wait, and starting again does not clear it:

```
a row of instance_deviations has a request_id that no row of
deviation_requests has; no release wrote one before this upgrade, so it was
written by hand. The upgrade stopped rather than change a compliance record:
set request_id to NULL on such rows, and it will finish when started again
```

No release before this one wrote `request_id`. Find the rows, read them, and
only then clear the column:

```sql
SELECT d.id, d.instance_id, d.kind, d.actor, d.request_id, d.created_at
FROM instance_deviations d
WHERE d.request_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM deviation_requests r WHERE r.id = d.request_id);
```

```sql
UPDATE instance_deviations d SET request_id = NULL
WHERE d.request_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM deviation_requests r WHERE r.id = d.request_id);
```

**What is different once it has run.**

- **A request has a deadline.** It waits 72 hours unless
  `METIS_DEVIATION_APPROVAL_TTL` says otherwise (a Go duration, such as
  `24h`). A value under `1h` is read as `1h` and one over `720h` as `720h`;
  one that is not a duration, or is not positive, is read as `72h`. Each of
  those is a warning when the server starts, naming the setting and what is
  used instead. The setting is read when a request is made and the deadline
  is stored with it, so changing it does not move a request that already
  waits.
- **A pass every ten minutes writes down what the clock has decided**, on
  every replica and every database, and once at start-up. A request past its
  deadline reads `expired`, and cannot be approved, whether or not the pass
  has run; the pass records it, and frees the step or the migration to be
  asked for again.
- **The ledger now holds rows for acts that were not made.** A waive that
  waits writes a row with `status: "pending_approval"`, and that row then
  reads `applied`, `rejected`, `expired` or `stale`. Every row was `applied`
  before. A client that counts each row of
  `GET /api/v1/instances/{id}/deviations` as something done must read
  `status`. A row that waited is also the one row that is changed after it is
  written: once, when its request is decided.
- **New fields on replies that shipped.** A migration's plan always carries
  `requires_second_approver`, and `second_approver_reasons` when it is true.
  The migrate reply always carries `passed_over_in_all`; `passed_over` lists
  at most 200 instances where it listed every one; each entry gains `cause`,
  `steps` and `steps_in_all` beside its `reason`. A waive's plan says
  `requires_second_approver: true`, where the field was always `false`.
- **Applying a migration in process refuses what the route sends for
  approval.** `ApplyInstanceMigration`, and `MigrateInstances`, which wraps
  it, answer a forbidden error for a plan that needs a second administrator
  unless the call carries the id of an approved request, which the gate reads
  from the database and never takes on the caller's word.
- **Who administers is written to the server's log.** Creating an account,
  changing its roles, changing its roles in one organization and deleting it
  each write one line naming who did it. See
  [the runbooks](runbooks.md#who-changed-who-administers).

**During a rolling upgrade or a canary** — read from the previous release's
code, not from two versions run side by side — a pod still on the previous
release knows nothing of this. The shipped deployment
(`deploy/kubernetes/metis.yaml`) recreates its pods, so the two run together
only where you changed that to roll, or
[run a canary](runbooks.md#rolling-out-through-a-canary).

- **Such a pod applies a waive, a skip and every migration on one
  administrator's call.** The control is in force only for a request a pod of
  this release serves. Finish the rollout before relying on it, and do not
  leave a canary of this release beside the previous one for longer than it
  takes to judge it.
- **The rows such a pod writes have no live key.** The application's own
  guard does not need one: every act first reads its visit's row under the
  instance's lock (`TestAnAppliedRowWithNoLiveKeyIsStillTheRecordOfItsVisit`).
  The database's unique index does, and does not see such a row. Each pod of
  this release fills the keys at start-up and on every pass of its first
  hour, and warns when it filled any: *Gave ledger rows written without it
  the key that holds their visit; a pod of an earlier release is, or was,
  writing to this database*. A row written after that hour stays without its
  key until a pod of this release next starts. To see them:

  ```sql
  SELECT id, instance_id, kind, status, actor, created_at
  FROM instance_deviations
  WHERE visit_key IS NOT NULL AND live_visit_key IS NULL
    AND status IN ('applied', 'pending_approval')
  ORDER BY created_at;
  ```

- **To find what was waived or skipped on one call in that window**, read the
  ledger for rows no request stands behind. Rows from before the upgrade are
  in it too, so bound it by when the rollout began:

  ```sql
  SELECT id, instance_id, kind, origin, node_name, actor, reason, created_at
  FROM instance_deviations
  WHERE kind IN ('waive', 'control_waived') AND status = 'applied'
    AND request_id IS NULL AND created_at >= '<when the rollout began>'
  ORDER BY created_at;
  ```

  A redirect leaves no ledger row under either release, so this does not find
  one.

**Rolling back.** The table and the column stay; the runner only goes
forward. Read from the previous release's code, not run:

- **A request that still waits holds its step for good.** The previous
  release has no approve route, no reject route and no pass that expires
  anything. It does find the waiting ledger row when a waive of that visit
  is applied: the same request sent again is answered `replayed: true` with
  `applied: false`, and any other request for the visit is refused *this
  step was already waived by …*, though nothing was waived. So **before
  rolling back, reject every request that waits, or let it expire**:

  ```sql
  SELECT id, kind, requested_by, reason, expires_at
  FROM deviation_requests
  WHERE status IN ('pending_approval', 'approved')
  ORDER BY created_at;
  ```

  `approved` is a migration whose run is going or never reported: let it
  finish, or wait out its hour, before the rollback.
- **The control is gone.** On the previous release a waive and a skip apply
  on one administrator's call again.
- **Rows it writes have no live key**, as above; the pods of this release
  fill them when you upgrade again.

**If your organization has one administrator.** Nothing is switched off for
you by default. An administrator's waive or skip becomes a request, and their
own approval of it is refused (403) in a sentence that begins *You asked for
this, and nobody else administers this organization, so it waits.* The
request waits until its deadline. The ways forward:

- **Appoint a second administrator: a second person's account.** Give them
  the Administrator role in the organization
  (`PUT /api/v1/users/{id}/organization-roles`) and have them approve. A
  second account held by the same person satisfies the check and defeats its
  purpose.
- **Use what still needs nobody else.** A cancel and a hold in place, a
  migration's `cancel` and `hold`, or letting the old version drain.
- **Withdraw the request** (the reject route, called by whoever asked) or let
  it expire.
- **Name the organization as having one administrator.** Whoever operates the
  installation sets `METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS` to the
  organization's id and restarts. The sole administrator may then approve
  their own request, with a reason, and the record says nobody else approved
  it. Read
  [An organization with one administrator](runbooks.md#an-organization-with-one-administrator)
  first: it says what this does not close.

**The second approver is not a control against an administrator who manages
accounts.** It protects against a mistake and against a decision nobody else
looked at. It does not protect against an administrator who creates, removes
or displaces accounts. Read
[What it does not protect against](process-change-in-flight.md#what-it-does-not-protect-against)
before describing it to an auditor.

**There is no approval screen.** The migration dialog says when an apply was
sent for approval. Approving and rejecting are calls to the API in this
release, and nobody is notified that a request waits: the requester passes
the request's id to the second administrator.

**What it costs.** Counted from the code, not measured. A waive's apply
writes a request, a waiting ledger row and a trail entry, and does nothing to
the instance; the approval then does what an apply did, and beside it writes
the approval's trail entry and the request's new status. A waive's preview
does one more read, of the visit's live row. A migration's plan reads nothing
it did not read before: what asks is worked out from the two definitions and
the instances the plan already had. An apply that runs under a request reads
the request once, under its row's lock, before it starts. Each ten-minute
pass makes two reads of the requests table per database when nothing is
overdue.

## An instance a migration left with nothing to do

No migration of the schema runs for this, and nothing changes for an instance
that is where a migration's plan found it. What changes is what an apply does
with an instance that moved while the migration was running, and that is in
[Changing a process that is already running](process-change-in-flight.md#node-actions--deciding-work-instead-of-moving-it):
such an instance is left on the version it is running and listed in
`passed_over`.

**What an earlier release could leave behind.** 0.3.0 and 0.4.0 checked where
an instance's work lands when they planned a migration, from one listing of the
instances, and not again when they rewrote each one (read from their code, not
run). An instance that reached, in between, a step the new version
does not have was moved onto the new version with its token, and its open task,
on that step. The apply reported nothing. It then went one of two ways, and a
query finds each. Both read only; run them on every database the server uses,
the main one and each environment's.

**1. Still holding the step.** The instance is `active` on a version that has
no step for a token it holds. If the step has a task, the task is still in its
holder's list, and completing it is what makes the instance unrecoverable, so
look for these first:

```sql
WITH RECURSIVE steps AS (
  SELECT d.id AS definition_id, n.step
    FROM process_definitions d
   CROSS JOIN LATERAL jsonb_array_elements(
           CASE WHEN jsonb_typeof(d.nodes::jsonb) = 'array'
                THEN d.nodes::jsonb ELSE '[]'::jsonb END) AS n(step)
  UNION ALL
  SELECT s.definition_id, n.step
    FROM steps s
   CROSS JOIN LATERAL jsonb_array_elements(
           CASE WHEN jsonb_typeof(s.step->'nodes') = 'array'
                THEN s.step->'nodes' ELSE '[]'::jsonb END) AS n(step)
), live AS (
  SELECT i.id, i.definition_id,
         CASE WHEN jsonb_typeof(i.tokens::jsonb) = 'array'
              THEN i.tokens::jsonb ELSE '[]'::jsonb END AS tokens
    FROM process_instances i
   WHERE i.status = 'active' AND i.deleted_at IS NULL
)
SELECT l.id, d.key AS process, d.version, t.token->>'node_id' AS step,
       (SELECT count(*) FROM tasks k
         WHERE k.instance_id = l.id AND k.deleted_at IS NULL
           AND k.node_id = t.token->>'node_id'
           AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated')) AS open_tasks
  FROM live l
  LEFT JOIN process_definitions d ON d.id = l.definition_id
 CROSS JOIN LATERAL jsonb_array_elements(l.tokens) AS t(token)
 WHERE NOT EXISTS (SELECT 1 FROM steps s
                    WHERE s.definition_id = l.definition_id
                      AND s.step->>'id' = t.token->>'node_id')
 ORDER BY l.id;
```

Each row is one token: the instance, the version it is on, the step that
version does not have, and how many tasks are open on that step. A step inside
a sub-process is found wherever it is nested. For each instance, before
anybody completes the task: plan a migration of that one instance back to a
version that has the step — the version it came from, named in its
`instance_migrated` trail entry — by sending `"instances": ["<id>"]` with the
two version ids, as a dry run first. The plan says whether it can be applied:
everything the instance holds must have a step on that version. Applied, the
instance is on that version at the step, and completing the step advances it
as that version says. Then run the migration you meant, with a mapping or a
decision for the step.

If the instance is not worth moving back, it can be closed where it is: a
cancel in place may name the step the instance holds a token on, though the
version it is on does not have that step
(`POST /api/v1/instances/{id}/deviations` with
`{"kind": "cancel", "node_id": "<the step from the query>", "reason": "…"}`,
previewed first). The plan and the record show the step by its id, since the
version has no name for it, and the cancel withdraws the open task and tells
its holder (`TestACancelCanNameAStepTheInstancesVersionNoLongerHas`, on an
instance put in this state through the repository: no release since creates
one). A cancel that names no step is refused, because the instance does hold a
token, and its refusal names the step by its id: *This instance is waiting at
“<step-id>”; say which of those steps it is to be ended at.* A waive and a hold
of that step are refused, *This process has no step "…".*: to do either, move
the instance back first, as above.

**2. With nothing left.** The step's holder completed the task. The token came
off, nothing followed because the version has no such step, and the instance
is `active` with no token, no open task, no job waiting or running, no work
parked for a worker, no event it is waiting for, no open incident and no
process it called still running:

```sql
WITH live AS (
  SELECT i.id, i.definition_id, i.updated_at,
         CASE WHEN jsonb_typeof(i.tokens::jsonb) = 'array'
              THEN i.tokens::jsonb ELSE '[]'::jsonb END AS tokens
    FROM process_instances i
   WHERE i.status = 'active' AND i.deleted_at IS NULL
)
SELECT l.id, d.key AS process, d.version, l.updated_at,
       EXISTS (SELECT 1 FROM audit_logs a
                WHERE a.instance_id = l.id AND a.deleted_at IS NULL
                  AND a.type = 'instance_migrated') AS migrated
  FROM live l LEFT JOIN process_definitions d ON d.id = l.definition_id
 WHERE jsonb_array_length(l.tokens) = 0
   AND NOT EXISTS (SELECT 1 FROM tasks k
                    WHERE k.instance_id = l.id AND k.deleted_at IS NULL
                      AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated'))
   AND NOT EXISTS (SELECT 1 FROM jobs j
                    WHERE j.instance_id = l.id AND j.deleted_at IS NULL
                      AND j.status IN ('pending', 'running'))
   AND NOT EXISTS (SELECT 1 FROM external_tasks x
                    WHERE x.instance_id = l.id AND x.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM process_instances c
                    WHERE c.parent_instance_id = l.id AND c.deleted_at IS NULL
                      AND c.status IN ('active', 'suspended'))
   AND NOT EXISTS (SELECT 1 FROM event_subscriptions s
                    WHERE s.instance_id = l.id AND s.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM incidents n
                    WHERE n.instance_id = l.id AND n.deleted_at IS NULL
                      AND n.status = 'open')
 ORDER BY l.updated_at;
```

`migrated` is true when the instance's trail has an `instance_migrated` entry.
An instance listed with it false was not moved by a migration whose entry was
kept, and got here some other way; the list is every running instance with
nothing in flight, whatever stranded it.

Nothing moves such an instance on. A completion needs a task. A migration's
`skip`, `cancel` and `hold` act on an instance that holds a token on the step
they name, and this one holds none; the step it was stranded on cannot be named
either, because the version it is on does not have it (*there is no node "…" in
the version being migrated from*). A migration that only moves work moves it to
another version as it is, with nothing to do there. It can be closed, and that
is a decision, not a repair:

- Read its trail to see what was done. For an instance a migration stranded,
  the last task completed is on the step the new version did not have, and
  whoever completed it gave that approval.
- If the business still needs what should have followed, start it again as a
  new instance.
- **Close it with a cancel in place that names no step.** An administrator of
  the instance's organization sends `POST /api/v1/instances/{id}/deviations`
  with `{"kind": "cancel", "reason": "…"}`. As it is, that is a dry run and
  answers the plan, which warns *This instance is not waiting at any step.
  Cancelling it closes it.* Sent again with the plan's `visit_key` and
  `"dry_run": false`, it closes the instance: its status is `cancelled`, and a
  row in its ledger and an `instance_cancelled` entry on its trail say who
  closed it and why, and name no step.
  [Closing an instance that has nothing left to do](runbooks.md#closing-an-instance-that-has-nothing-left-to-do)
  has the calls. This replaces the direct `UPDATE` of the row that this page
  gave as an unsupported last resort, which left no record of who decided.
- If the instance was called by another process (`parent_instance_id` is
  set), that parent is still waiting for it and is not resumed by closing it.
  The plan says so. Cancel or hold the parent next.
- Left as it is, the instance has nothing in flight and nothing will run for
  it; it goes on showing as running in every list and every count.

Both queries were run with `psql` against a schema with this release's tables,
over temporary tables holding rows each must list and rows each must not: for the
second, an instance with a token, with an open task, with a pending and with a
running job, with parked work, with a waiting event, with an open incident,
with an active child, one that is finished and one that is deleted are left
out, and one with only finished, resolved or deleted things around it is
listed; for the first, a token on a step nested two sub-processes deep is left
out. Neither was run against an installation that an earlier release had
stranded an instance in.

**Rolling back** needs nothing: no schema changed. The earlier release applies
a migration as it always did, with the window described above.

## A task a migration reopened

No migration of the schema runs for this either. What changes is in
[Changing a process that is already running](process-change-in-flight.md#re-derived-assignment):
a migration with a node mapping now rebuilds only the tasks that are still
open. A completed or cancelled one keeps everything that says what happened,
and takes its step's new id only where the mapping renames the step.

**What an earlier release could leave behind.** 0.4.0 applied the mapping to
every task of the instance on a mapped step, whatever its status, and a task
that changes step is rebuilt from the step it lands on and offered again (read
from its code, not run; 0.3.0 changed only the step's id on such a task and
left its status alone, also read, so it did not reopen one). So any instance
that was moved by a migration with a mapping, and had already finished a task
on one of the mapped steps, has that task back as open work: on the step the
mapping named, with that step's name, form and candidates, assigned to whoever
that step names or to nobody. Nothing has to have raced; it is what a mapping
did. An instance migrated with `opsApprove → salesApprove` after its operations
approval had been given has two open sales approvals where it should have one,
and one further on has an open task on a step it is no longer at.

The task row itself no longer says what it was. The trail does, in the columns
that are stored in the clear. An entry's `data` is **not** one of them: the
server encrypts it, in 0.4.0 as now, so nothing in it can be read in SQL — the
first version of this query read the re-pointed steps and the person from
there and listed nothing on any real database. What can be read:

- a `task_completed` entry, or the `TaskCanceled` entry of a task the engine
  withdrew, names the step in `node_id` and `node_name`, and a completion's
  `narrative` begins with who did it (*ollie completed task "Operations
  approve"*);
- the migration's `instance_migrated` entry says in its `narrative` which
  version it moved the instance from and which steps it re-pointed (*…from
  version 1 to version 2 of "quotation" by a migration, authorised by dita.
  Work in progress was re-pointed: opsApprove→salesApprove.*). 0.4.0 listed
  there every step that had a task, finished ones included.

This finds an open task that was created before a completion or a withdrawal
on a step, where a later migration then re-pointed that step onto the step the
task is on now. Read only; run it on every database the server uses.

```sql
WITH RECURSIVE steps AS (
  SELECT d.project_id, d.key, d.version, n.step
    FROM process_definitions d
   CROSS JOIN LATERAL jsonb_array_elements(
           CASE WHEN jsonb_typeof(d.nodes::jsonb) = 'array'
                THEN d.nodes::jsonb ELSE '[]'::jsonb END) AS n(step)
  UNION ALL
  SELECT s.project_id, s.key, s.version, n.step
    FROM steps s
   CROSS JOIN LATERAL jsonb_array_elements(
           CASE WHEN jsonb_typeof(s.step->'nodes') = 'array'
                THEN s.step->'nodes' ELSE '[]'::jsonb END) AS n(step)
), migrated AS (
  SELECT m.instance_id, m.created_at,
         substring(m.narrative from 'from version ([0-9]+) to version')::bigint AS from_version,
         substring(m.narrative from ' of "(.*)" by a migration, authorised by ') AS process,
         mv.move
    FROM audit_logs m
   CROSS JOIN LATERAL regexp_split_to_table(
           substring(m.narrative from 'Work in progress was re-pointed: (.*?)\.(?: It had not yet passed |$)'),
           ', ') AS mv(move)
   WHERE m.type = 'instance_migrated' AND m.deleted_at IS NULL
)
SELECT k.id AS task_id, k.instance_id,
       k.node_id AS step_now, k.name AS name_now, k.status AS status_now, k.assignee AS assignee_now,
       c.node_id AS step_it_was_on, c.node_name AS name_it_had,
       CASE c.type WHEN 'task_completed' THEN 'completed' ELSE 'canceled' END AS it_was,
       substring(c.narrative from '^(.*) completed task "') AS by_whom,
       c.created_at AS at, m.created_at AS reopened_at,
       CASE WHEN EXISTS (SELECT 1 FROM steps s
                          WHERE s.project_id = k.project_id AND s.key = m.process
                            AND s.version = m.from_version AND s.step->>'id' = k.node_id)
              OR (SELECT count(*) FROM migrated o
                   WHERE o.instance_id = m.instance_id AND o.created_at = m.created_at
                     AND split_part(o.move, '→', 2) = k.node_id) > 1
            THEN 'redirect' ELSE 'rename' END AS mapping_was
  FROM tasks k
  JOIN migrated m
    ON m.instance_id = k.instance_id AND m.created_at > k.created_at
   AND split_part(m.move, '→', 2) = k.node_id
  JOIN audit_logs c
    ON c.instance_id = k.instance_id AND c.deleted_at IS NULL
   AND c.type IN ('task_completed', 'TaskCanceled')
   AND c.node_id = split_part(m.move, '→', 1)
   AND c.created_at > k.created_at AND c.created_at < m.created_at
 WHERE k.deleted_at IS NULL
   AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated')
 ORDER BY k.instance_id, k.created_at, c.created_at;
```

Each row is a task that is open and should not be: where it is now, the step it
was on and the name it had, whether it had been completed or withdrawn, by whom
and when (`by_whom` is empty for a withdrawn task: the trail does not say who
it had been with), and when the migration reopened it. `mapping_was` says what
kind of mapping moved it, because that decides where it belongs now:

- `redirect`: the step it is on now was a step of the version it came from as
  well (or several of this instance's steps were sent to it). The work was done
  on the old step and on no other; the task goes back to that step's id.
- `rename`: the step it is on now is new in the version it was moved to, and
  stands for the step it was on. The task stays on the new id, which is where
  this release puts a finished task under such a mapping, so that a rule of
  the new version naming the step finds who performed it.

**What the query can and cannot see.**

- It reads sentences. The two it parses are written by 0.4.0 and by this
  release in the same words (read from both). An installation that has changed
  them, or a release that words them differently, is not matched.
- It sees only the steps the migration's sentence names, which are the steps
  that had a task of that instance. So `mapping_was` judges "several steps onto
  one" from this instance's tasks alone, and a mapping that sent two steps to
  one new id reads as a rename where the instance had a task on only one of
  them. Check it against the mapping that was applied.
- The trail does not name tasks, only the step and the instance, so the match
  is by step and time, and it is wrong two ways. On a step that runs once per
  item, an item still open when the migration ran is listed if another item of
  the same step had been completed after it was created. And an open task on
  the step mapped to, created before a completion on the step mapped from on a
  parallel branch, is listed too. Both have a token under them and were never
  completed: check a listed task against its instance before touching it.
- It needs the entries to exist. One the server logged as lost is not there to
  be found, and neither is a withdrawal on an installation whose trail does not
  record them (the entry is written by the audit observer the server registers
  at start).
- A step id that contains `→` or `, ` is not split correctly.

**Do not complete a listed task.** Run, with rows shaped as 0.4.0 leaves them:
completing a reopened task that has no token under it was accepted, and
advanced the instance a second time from that step — a second token, and a
second open task, on the step after it. Where the reopened task shares a step
with the genuine one, either can be completed and the instance advances once;
the other then stays open on an instance that has moved on, and completing it
later is accepted too.

**There is no supported way to put one back.** Nothing in the product
withdraws or closes a single task. A migration's `skip` and `cancel` act on an
instance that holds a token on the step they name, so they do not reach a
reopened task with no token under it, and where there is a token they withdraw
every open task on the step, the genuine one included. The waive and the
cancel of one instance in place, new in this release, are no different: a waive
is refused where the instance holds no token on the step and otherwise
withdraws every open task on it, and a cancel ends the whole instance. An
audited way to close one task is in the roadmap.

**Unsupported, as a last resort:** the row can be put back in the database from
what the query listed,

```sql
UPDATE tasks
   SET status = 'completed',        -- or 'canceled', as it_was says
       node_id = '<step_it_was_on>', -- for a redirect; for a rename leave this line out
       name = '<name_it_had>',
       assignee = '<by_whom>'       -- for a canceled task leave this line out
 WHERE id = '<task_id>'
   AND status IN ('unclaimed', 'claimed', 'delegated', 'escalated');
```

one task at a time, in a transaction, after a backup, committing only when it
reports exactly one row changed. It restores what the trail kept: the status,
the step, the step's name, and who completed it. Who a withdrawn task had been
with is not kept, so its assignee stays whoever the step it was moved to
names. What the task's form,
description, priority, due date and candidates were before the migration is
not kept anywhere, and they stay those of the step it was moved to. Nothing is
written to the trail or the ledger by it, so record what was changed, by whom
and why, outside the product. After it the task is out of its holder's list
and the instance is not touched.

**How this was checked.** `TestTheUpgradingQueryFindsTheTasksAnEarlierReleaseReopened`
(`tests/instancemigration`) reads the query out of this page and runs it, as
printed, over rows written through the service and the repository, so that
`data` is encrypted as it is on an installation: instances whose tasks were
completed, and withdrawn by a boundary event, by the server; then moved the way
0.4.0's rewrite moved them — every task on a mapped step rebuilt and offered
again, the entry written with 0.4.0's sentence — for a redirect and for a
rename. It lists the three reopened tasks with the step, the name, the person
and the kind of mapping, and does not list the genuine task created after the
completion, an open task that was moved and never completed, or anything of an
instance this release migrated the same way. Put back with the `UPDATE`, each
task is the row it was before, and the query lists nothing. It was also run
once, by hand, over an instance the service migrated as it was before this
change, when it still reopened tasks as 0.4.0 does: it listed the one reopened
task, on the step it had been on, with the person who had completed it. And it
was run, read only, against a schema as an installation has it (`data` is a
`jsonb` column there and a text one in the test's; the query reads neither).
It was not run against an installation that 0.4.0 itself had migrated.

**Rolling back** needs nothing: no schema changed. 0.4.0 applies a mapping as
it did.

## Migration 33: an instance's ledger of what was done to it

An instance now keeps a row for each thing done to it that its process did not
decide, and the upgrade runs migration 33 to make the table. What writes a row,
and who reads it, is in the changelog and in
[Watching instances](integration.md#watching-instances); this is what the
upgrade does and what it changes for you.

**What migration 33 does.** It creates `instance_deviations` and its indexes.
Nothing is backfilled: what was done to an instance before the upgrade is in
its audit trail and stays there, and its entries carry no `deviation_id`.
Every statement is `IF NOT EXISTS`, so a run that stops part-way finishes when
the server is started again. The table has foreign keys to `projects` and to
`process_definitions`, and none to `process_instances` or `tasks`, on purpose:
a hand-over holds the task's row, a completion holds the instance's and then
waits for the task's, and a reference from the ledger to either would make the
hand-over's insert wait for a lock the completion holds. Whoever reconciles
the storm model's own table definitions with the schema the migrations build
must not add those two.

**It can stop the upgrade, on purpose.** Creating the table needs a brief lock
on `projects` and on `process_definitions`, and while that waits for a long
query PostgreSQL queues every later write to either table behind it. The
migration waits two seconds and then stops, saying

```
projects or process_definitions was held for more than 2s by a long query or
transaction; the upgrade stopped rather than hold every writer of either
behind it, and will finish when started again once that ends
```

followed by the database's own error. Start the server again once the query
has ended and it finishes; nothing it had done is undone.

**What is different once it has run.**

- A change that must be recorded is not made if its row cannot be written. A
  hand-over or an edit by somebody who does not hold the task is refused with
  *the change was not recorded in the deviation ledger, so it was not made*
  and the task stays as it was. A migration's skip, cancel or hold fails that
  instance's step, names the instance, and says how many had been dealt with;
  running the same migration again carries on. An ad-hoc activation is
  refused and starts nothing. A migration's skip, cancel and hold used to be
  made and the lost entry only logged.
- A migration's skip, cancel or hold leaves alone an instance that left the
  step between the migration listing its instances and locking that one — its
  holder completed the step, or it finished. Nothing is done to it or recorded
  about it, and it is not moved to the new version in that run; it stays on
  the version it is running for the next run of the same migration. The reply
  to an apply lists it in `passed_over`, with the reason — a new field, always
  present, `[]` when nobody was left behind — and `applied` is now `false`
  when the run passed instances over and acted on none. A
  skip used to advance such an instance a second time, and its ledger row said
  the approval was waived. See [Node actions](process-change-in-flight.md#node-actions--deciding-work-instead-of-moving-it).
- A row says the act was made, and is never rewritten. (Since migration 34
  there is one exception: a row that waits for a second administrator says a
  waive was asked for, and is rewritten once, when its request is decided.
  See [above](#a-second-administrator-approves-waivers-and-skips-migration-34).)
  A `hold` row therefore
  says the hold was placed, not that it is still open: resolving the incident
  the hold raised writes no row and leaves `after.incident.status` reading
  `open`. Read the instance's incidents, `GET /api/v1/incidents/{instanceId}`,
  for the one whose `id` is the row's `after.incident.id`. Resolving an
  incident, sending a message or a signal, an operator claiming a task nobody
  was named for, and the engine withdrawing tasks write no row, by decision;
  [The ledger](process-change-in-flight.md#audit) says why for each.
- Only `before` and `after` are sealed. `reason`, `actor`, `node_name` and
  `details` are stored in plain text, as the audit trail's sentence, which
  already holds the reason, is.
- Reading an instance's ledger needs the `ENCRYPTION_KEY` its rows were
  written under, because `before` and `after` are sealed as every other copy
  of a process variable is. A backup without that key restores rows whose
  `before` and `after` cannot be read, and reading such an instance's ledger
  fails rather than return them as they are stored. `metis --reseal` takes
  them along with the other sealed columns: it walks every text, JSON and
  binary column of the schema, and this table's are among them. That is read from
  the code in `internal/app/reseal.go`, not run against a ledger.
- A reason on a migration's `skip`, `cancel` or `hold` of more than 2,000
  characters is a refusal in the plan, so a dry run shows it.
  `POST /api/v1/processes/adhoc/activate` takes an optional `reason`, at most
  2,000 characters.

**During a rolling upgrade** — read from the previous release's code, not
from a rollout of two versions run side by side — a pod still on the old
release does not know the table. A hand-over, a migration or an activation it
serves writes no row, so the ledger of an instance touched in that window is
missing the act, which its audit trail still shows. Finish the rollout before
relying on the ledger being complete.

**Rolling back** leaves the table and its rows where they are. From reading
the old release's code, not from running it, its application code does not
read or write them, but its `metis --reseal` walks every column of the schema
from the catalogue, so it would read the ledger's sealed `before` and `after`.
It rewrites only the values sealed under a previous key, under the current
one; a value already under the current key is counted and left as it is. As
with every migration, the runner only goes forward.

**What it costs.** Counted from the code, not measured: a hand-over or an edit
by somebody who does not hold the task, each decision a migration makes on an
instance, and each ad-hoc activation do one more read, that the instance
belongs to the project the row names, and one insert, inside the transaction
that is already open. Each accepted control loss is the same again, one more
read and one more insert for each control step the instance loses, and an
ad-hoc activation also writes a `step_activated` trail entry, which it had none
of before. The check that the project is the caller's is answered
from what the request already looked up. A hand-over by the task's holder, and
a migration that only moves work and waives no control, do neither. It is a person's action or an
administrator's migration, not something the engine does for every token.

## Handing a task over is checked and recorded

Assigning, delegating, releasing, handing back and editing a task are held to
rules they did not have, a delegation now goes back to whoever made it, and the
upgrade runs migration 32.

**What is refused that was not.**

- A hand-over to a name that is not an account in the task's organization: 400,
  *there is nobody called "…" in this organization to hand the task to*. The
  same words for a name nobody has and for another organization's member.
- A hand-over to somebody who already did a step the task's
  `separation_of_duties` names: 400, for an administrator as for anybody. When
  the step the task belongs to is no longer part of its process, nobody can
  check that rule and the task cannot be handed to anyone until it is migrated.
- A hand-over of a task offered to people or teams to somebody who is not one
  of them: 400 for its holder. An administrator may do it with a `reason`, and
  the audit entry says it went to somebody the task was not offered to.
- Any assign, delegate, release, hand back or edit by somebody who does not
  hold the task — an administrator, or an operator handing on a task nobody was
  named for — without a `reason`: 400, *say why you are …*. The person holding
  the task is not asked, except an administrator who sends it to somebody it
  was not offered to.
- Completing a task that has been delegated and not handed back: 403, to its
  owner as to anybody. Releasing, assigning or delegating it again: 400 for its
  delegate or an administrator, who are told to hand it back first; 403 for
  anybody else, its owner included.
- Delegating a task nobody holds: 400. Claim or assign it first.
- Handing a task back that has not been delegated: 400. Handing one back as
  somebody other than its delegate or an administrator — its owner included:
  403.
- A hand-over to the person who already holds the task, by assign or by
  delegate: 400, *… already holds this task*. It used to answer 200. A retry,
  or a step that makes sure the assignee is somebody, can read that reply as
  done.
- Releasing a task that is not claimed — nobody holds it, it is completed or
  withdrawn, or it is with a delegate — by its holder or an administrator:
  400, saying which. It used to be a server error (500). Anybody else gets the
  403 they always did.
- A request body that is not JSON, or that carries a field of the wrong kind
  (`{"user_id": 5}`), on assign, delegate, release, hand back and edit: 400,
  in a sentence that does not repeat the decoder's error. On assign, delegate
  and edit it used to be a server error, and a release never read its body.
- A completion sent at the same moment as a hand-over of the same task. Both
  used to be answered 200, and the completion won: the task was completed by,
  and held again by, the person it had just been taken from. Now whichever
  takes the task's row first is made, and the other is refused for what the
  task has become: a hand-over of a completed task is a 400, a completion by
  somebody who no longer holds it a 403.

**What changes for an integration.**

- Send `"reason": "…"` (at most 1000 characters) wherever it acts on tasks it
  does not hold:
  `{"user_id": "citra", "reason": "budi is on leave until Monday"}`.
- Releasing over Connect or gRPC carries no reason, so there only the person
  holding the task can release it. An administrator who does not hold it uses
  `POST /api/v1/tasks/{id}/unclaim` with a `reason`.
- Where it delegates, expect the delegate to hand back with
  `POST /api/v1/tasks/{id}/resolve` and the original holder to complete.
  `GET /api/v1/tasks/delegated` lists what the caller delegated that is still
  with a delegate. A task now carries `owner` and `delegation_state`
  (`pending` while it is with the delegate, `resolved` once handed back).
- Where it edits, send only the fields it means to change. A field left out of
  `PUT /api/v1/tasks/{id}` is now left as it is, where it used to blank the
  name and zero the priority; to remove a due date send `"due_date": null` (or
  `""`). A name is trimmed, and a blank one is refused with a 400. A delegate
  holds the task while it is with them, so they may change its name, priority
  and due date as any holder may; each change is recorded with who made it.
- An administrator who names nobody to hand a task to, on a task that does not
  exist or is in another organization, gets a 404 where it used to be a 400.
- Where it listens for a delegation, the event is now `TaskDelegated`; it used
  to arrive as `TaskUpdated`, which a subscriber that keyed on it no longer
  sees for a delegation. A hand-back is `TaskResolved`, which is new: there was
  no hand-back before. Both carry the person the task went to as `assignee`.
  Two more things changed. An assignment is still `TaskClaimed`, and now
  carries the person the task went to as a top-level `assignee` as well as in
  `variables.assignee`. And an edit that changes nothing — a `PUT` that
  resends the values the task already has — writes nothing and raises no
  `TaskUpdated`, where every `PUT` used to raise one. A release is still
  `TaskUpdated`.
- `owner` and `delegation_state` are sent, over REST and over Connect and gRPC,
  only when they mean something: a client reading `delegation_state: "pending"`
  can rely on the task being with a delegate, and `resolved` is kept. A stale
  mark left on a row by a pod of the previous release is not sent.

**What the audit trail says.** Each hand-over is an entry whose `actor` is the
caller — never the person it went to — with `previous_holder`, `target`,
`reason` and, for a delegation, `owner` beside it. The types are
`task_assigned`, `task_delegated`, `task_resolved`, `task_unclaimed` and
`task_edited`; an edit carries each changed field's value before and after.
Entries written before the upgrade keep the sentence they had. The entry is
written in the transaction that moves the task: when it cannot be written the
task is not moved.

**Notifications.** The person a task is assigned or delegated to is told, and
so is an owner when the task is handed back or when the engine or a migration
withdraws a delegation they made. A migration that retargets a delegated task
tells nobody. The words of a notification are written by the server in English.

**Migration 32** adds `tasks.owner` and `tasks.delegation_state`, both
nullable, converts the delegations that already exist, and builds
`ix_tasks_owner` without locking the table. Those delegations have no owner —
none was recorded — so each becomes a claim by its assignee, which is what it
was in effect (one with no assignee goes back to the queue). They are converted
5,000 at a time, each batch its own short transaction; soft-deleted rows are
converted too, which the query below leaves out because nobody can see them.
The conversion changes the status alone: it writes no audit entry and leaves
`updated_at` as it was, so the query's result is the only record of which tasks
they were. To see the live ones before upgrading:

```sql
SELECT id, name, assignee, instance_id, created_at
FROM tasks
WHERE deleted_at IS NULL AND status = 'delegated';
```

Adding the columns needs `tasks` to itself for a moment. The migration waits
two seconds for it and then stops, saying

```
tasks was held for more than 2s by a long query or transaction; the upgrade
stopped rather than hold every inbox and every completion behind it, and will
finish when started again once that ends
```

rather than queue every inbox and every completion behind a long query. The
conversion waits two seconds for a delegated row that another transaction holds,
and stops the same way, saying *a delegated task was held for more than 2s*.
Start the server again once that query has ended and it finishes; nothing it
had done is undone.

**During a rolling upgrade** — this is read from the previous release's code,
not from a rollout of two versions run side by side — a pod still on the old
release lets a delegate
complete or hand on a task the new release marks as pending, and a delegation
an old pod makes has no owner, so it stays its assignee's to complete. Finish
the rollout before relying on delegation. To find delegations that have no owner
once it is done:

```sql
SELECT id, name, assignee, instance_id, created_at
FROM tasks
WHERE deleted_at IS NULL AND status = 'delegated' AND COALESCE(owner, '') = '';
```

**Rolling back** after delegations exist leaves them as they are in the table.
From reading the old release's code, not from running it, it treats each as it
always did, a task its delegate may complete,
with nobody to hand it back to.

**A delegation whose owner has left.** Handing a task back goes to the owner's
name whether or not it still has an account, so the delegate is never stuck. The
task is then that name's claim, which nobody can complete: an administrator
assigns it to somebody, or releases it, with a reason. To find them:

```sql
SELECT t.id, t.name, t.assignee AS delegate, t.owner
FROM tasks t
WHERE t.deleted_at IS NULL AND t.status = 'delegated' AND t.delegation_state = 'pending'
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.username = t.owner AND u.deleted_at IS NULL);
```

**What it costs.** A hand-over reads more than it did, under the task's row
lock, because it now checks who the task goes to: the account, the task's
project, the step's definition (through the instance), the account's groups
when the task is offered to groups, and, for a step with a separation-of-duties
rule, the instance's tasks. A count of 8 reads before and 13 to 15 after was
taken once during development from `pg_stat_user_tables` with a throwaway test
that is not in the repository. It is a person's action rather than something a
process does, and it is not run in bulk.

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

**Only approvals are counted this way.** The change applies to a user task or a
manual task that runs once per item. Every other step that runs once per item —
a sub-process, an external task, a call activity, a service task, a script — is
counted exactly as it was before the upgrade, and an instance that is inside
one carries on as it would have. That includes what was already loose about
them, which this release does not fix:

- an external task or a call activity that runs once per item leaves its
  tokens on the step when it finishes, so its process moves on and then never
  completes;
- when a completion condition ends one of them early, or a deadline interrupts
  it, the work its other runs have under way is not withdrawn — the external
  tasks stay on offer, the called processes run on, the queued service calls
  are made — and that work is accepted when it comes back, which can move the
  process on from a step it has left;
- a sub-process run once per item in parallel does not finish when it has a
  service call inside, or three or more waiting steps inside whose runs
  overlap, or an approval inside that itself runs once per person (only the
  first item's approvers are asked): the instance stays `active` with nothing
  open, and no query below lists it;
- a deadline on a sub-process that runs once per item never fires.

Fixing these needs each run of a repeating sub-process to have tokens of its
own; that is the follow-up, and the strict counting of approvals is extended to
every step after it.

**Processes this had already stranded are not repaired.** An instance whose
approvers had all answered before the upgrade is `active`, holds tokens on the
approval, and has nothing open: no task, no job waiting or running (a timer or a deadline
is a waiting job), no work parked for a worker, no process it called still
running, no event it is waiting for, and no open incident. An instance with an
open incident is waiting on an operator and is not listed, and neither is one
whose approval still has a deadline or an event pending: it shows up once
that has passed or been dealt with. This finds them:

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
                      AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated'))
   AND NOT EXISTS (SELECT 1 FROM jobs j
                    WHERE j.instance_id = l.id AND j.deleted_at IS NULL
                      AND j.status IN ('pending', 'running'))
   AND NOT EXISTS (SELECT 1 FROM external_tasks x
                    WHERE x.instance_id = l.id AND x.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM process_instances c
                    WHERE c.parent_instance_id = l.id AND c.deleted_at IS NULL
                      AND c.status IN ('active', 'suspended'))
   AND NOT EXISTS (SELECT 1 FROM event_subscriptions s
                    WHERE s.instance_id = l.id AND s.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM incidents n
                    WHERE n.instance_id = l.id AND n.deleted_at IS NULL
                      AND n.status = 'open');
```

Every row is an instance holding only iteration tokens with nothing in flight
for any of them. The list is not only approvals from before the upgrade: an
external task or a call activity that runs once per item strands its process
the same way, before the upgrade and after it, so run the query again from time
to time if you use those. Look at each before you end it: check the incident
list for the instance first, and open the instance to see what its step was
waiting for. Whether the business was in fact finished is a decision, not
a repair: end the ones that were with a migration's *End the instance* action
(`cancel`, in `docs/process-change-in-flight.md`), which records who decided
and why.

**"Two of three" now withdraws the third.** An approval whose completion
condition is met withdraws the approvals still open and tells their holders.
Before the upgrade those tasks were left open; one left open by an *earlier*
early finish is refused (400 over REST) when somebody completes it — *this step
has already finished and the process has moved on* — and stays in their list.
A deadline still pending on such an approval no longer fires; before the
upgrade it did, and took the process down its deadline path from a step it had
left. The tasks are the open tasks of a step the instance no longer counts but
still holds run tokens for. Run this once every server is on the new release:
while the two releases run side by side, a server still on the old one can end
an approval early and leave tasks behind that a new one created.

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
 WHERE k.deleted_at IS NULL
   AND k.status IN ('unclaimed', 'claimed', 'delegated', 'escalated')
   AND NOT l.counting ? k.node_id
   AND EXISTS (SELECT 1 FROM jsonb_array_elements(l.tokens) t
                WHERE t->>'node_id' = k.node_id AND coalesce(t->>'iteration_id', '') <> '');
```

Set them `canceled` once you have looked at them. The step's tokens stay where
the old release left them, so once its tasks are closed and the instance has
nothing else in flight it appears in the first list, to be looked at and ended
like the others.

**An approval a deadline interrupted before the upgrade is still counting.**
Before the upgrade an interrupting deadline (or any other interrupting boundary
event) on an approval took its tokens and its tasks and left its count; from
this release it takes the count too. An instance interrupted before the upgrade
keeps the count it was left with. That matters only if the process comes back
to the approval — "chase, then ask again": the step is entered as if it were
already running and asks nobody, exactly as before the upgrade, and sits there
until its boundary event fires again. When it does, the approval ends whole,
and the next time the process reaches it everybody is asked. If the boundary
event is a deadline that is a wait of one more deadline; if it is a message or
a signal that may never come, the instance stays on the approval with nothing
open, and neither query above lists it. This lists the instances that still
carry such a count, on the approval or elsewhere:

```sql
WITH live AS (
  SELECT i.id,
         CASE WHEN jsonb_typeof(i.tokens::jsonb) = 'array'
              THEN i.tokens::jsonb ELSE '[]'::jsonb END AS tokens,
         CASE WHEN jsonb_typeof(i.multi_instance::jsonb) = 'object'
              THEN i.multi_instance::jsonb ELSE '{}'::jsonb END AS counting
    FROM process_instances i
   WHERE i.status = 'active' AND i.deleted_at IS NULL
)
SELECT l.id, c.node_id,
       EXISTS (SELECT 1 FROM jsonb_array_elements(l.tokens) t
                WHERE t->>'node_id' = c.node_id) AS on_the_step
  FROM live l CROSS JOIN LATERAL jsonb_object_keys(l.counting) AS c(node_id)
 WHERE EXISTS (SELECT 1 FROM tasks k
                WHERE k.instance_id = l.id AND k.node_id = c.node_id AND k.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(l.tokens) t
                    WHERE t->>'node_id' = c.node_id AND coalesce(t->>'iteration_id', '') <> '');
```

A row with `on_the_step` false is elsewhere in its process and needs nothing
unless it comes back. A row with it true is on the approval asking nobody: let
its deadline pass, or, if nothing will fire, end the instance or move it with a
migration as you would have before the upgrade.

**An approval now also finishes when everybody asked has answered**, whatever
its condition says. A condition that the list could not satisfy used to hold
the step for ever; check any condition on a repeating user task or manual task
that was relying on that. On every other repeating step a completion condition
still replaces "everybody has answered", as before.

**Versions imported from BPMN before this release** keep a multi-instance
completion condition where nothing evaluates it, and go on running all-of-N —
changing a deployed version under its running instances is not something an
upgrade should do. For an approval — a user task or a manual task — import the
file again (or export and re-import the version) to deploy one that honours
the condition. For every other repeating step importing again changes nothing:
in this release an imported completion condition takes effect on approvals
only, and on a service task, an external task, a call activity or a script it
stays where it was, not evaluated, and the step runs for every item.

**A file whose completion condition on an approval cannot be read is now
refused on import.** A completion condition is read in Metis's own expression
language (FEEL): for example `nrOfCompletedInstances >= 2`, or
`nrOfCompletedInstances / nrOfInstances >= 0.6`. The marking another modeler
puts around an expression is taken off — `${…}` and `#{…}` (Camunda 7,
Flowable), a leading `=` (Camunda 8) — so `${nrOfCompletedInstances >= 2}` now
works where it used to be imported and never hold. What is inside is not
translated: a condition written with `==`, `&&` or a method call is refused
(400 over REST) with a message naming the step, where it used to import and run
the step for everybody. Rewrite the condition and import again. Two kinds of
condition are still imported and then never hold — the approval then finishes
only when everybody has answered — so check for them by eye: one written
against Camunda 8's counter names (`numberOfInstances`,
`numberOfCompletedInstances` and the like — Metis provides `nrOfInstances`,
`nrOfCompletedInstances`, `nrOfActiveInstances`), and one that uses a single
`=` to compare with something other than a plain value
(`nrOfCompletedInstances = nrOfInstances - 1`; write `>=`, or compare with a
number). A file is not refused for a condition on a step that is not an
approval, readable or not. Conditions on sequence flows are imported as
written, as before; a gateway that cannot choose a flow raises an error when
it is reached.

**Ad-hoc sub-processes** now withdraw the steps still running inside them when
their completion condition is met, at any depth, with the tasks, the work
waiting for workers, and the events those steps were waiting for. A worker
still holding work for one of those steps is told there is no such external
task when it reports, and the instance's history has a `parked_work_withdrawn`
entry for the step; a service call still queued for one is not made. A process
one of those steps called is not ended: it runs on, and its tasks stay open
until somebody completes them. A sub-process that should wait for its steps
instead says `cancelRemainingInstances="false"` in its BPMN file.

An ad-hoc sub-process with **no** completion condition has always finished when
the first step inside it does. It now also withdraws every other step that was
started and is still open — before, they were left in people's inboxes. If the
steps are all meant to be done, give the sub-process a completion condition
that says so, or `cancelRemainingInstances="false"`.

With `cancelRemainingInstances="false"`, do not start the same step twice
while the first is still open. A step holds one place however often it is
started, so when the first of the two finishes the sub-process no longer sees
the second: it can move on with the second one's task still open. This is
not new, and it is on the roadmap.

**Rolling back** to the previous release is safe for the data: the column is
nullable and the previous release ignores it. The defects come back with it:
approvals completed from the inbox while it runs leave their tokens behind
again, and an approval ended early leaves its other tasks open. Upgrading again
does not re-run migration 31; run the queries above again when you do.

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
