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
waiting, correctly. Check the task actually reaches somebody:

```sql
SELECT node_id, type, assignee, candidate_users, candidate_groups
FROM tasks WHERE instance_id = '<instance-id>' AND status <> 'completed';
```

If it names a group nobody is in, add a member. The task then appears in their
inbox with no further action.

If it names nobody — no assignee, empty candidate lists — it is waiting for an
administrator or an operator. Only they may take it, and it is under
*Available to Claim* in their inbox; everybody else is refused it with a 403
that says so. One of them can claim it, or give it to the person it should go
to:

```bash
curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"user_id": "<username>", "reason": "<why it goes to them>"}' "$METIS/api/v1/tasks/<task-id>/assign"
```

The reason is required — you do not hold the task — and is kept in the
instance's audit trail with your name.

Then fix the step in the designer, which warns about it, so the next instance
names somebody — a user step or a manual one, under *Who does this*.
`METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true` lets anybody take these tasks again
for a migration window; see [`upgrading.md`](upgrading.md).

If whoever completes it — usually an integration — is refused with *this
task's form has no field named …* or *this task has no form to declare …*, the
completion carried a variable the task's form does not declare, and nothing
was changed. The form the task was created with, which is the one it answers
to:

```sql
SELECT node_id, form_key, form_definition FROM tasks WHERE id = '<task-id>';
```

If the step should set that variable, give its form the field in the designer
and deploy; the task already waiting keeps its form, so complete it with that
form's fields. If it should not, the integration has to stop sending it.
`METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true` lets such completions through for
a migration window; see [Completing a task sets only what its form
declares](upgrading.md#completing-a-task-sets-only-what-its-form-declares).

### It is waiting on a job that keeps failing

See [A poison job](#a-poison-job).

### It raised an incident

The engine refuses to guess. A gateway with no matching condition and no default
flow raises an incident and stops, deliberately — that is not a bug, it is the
alternative to sending somebody's approval down an arbitrary branch.

```bash
curl -sH "Authorization: Bearer $TOKEN" \
  "$METIS/api/v1/incidents/<instance-id>" | jq '.incidents[] | {id, step: .node.id, error, status, created_at}'
```

The incident inbox in the interface shows the same thing with the cause in plain
words and a **Try again** button, which is the supported fix. Use it after correcting
whatever the message names. If the cause was the process model itself, deploy a
corrected version — running instances continue on the version they started on.

An incident whose text begins *held at* is not a failure. An administrator held
the instance there for somebody to decide; the text says who and why. See
[Holding an instance, and letting it go](#holding-an-instance-and-letting-it-go).

### It is waiting on nothing

`active`, with no token, no open task, no job waiting or running, nothing
parked for a worker and no event it waits for: nothing will run for it, and it
shows as running in every list. A migration of an earlier
release could leave one, and so does a process whose last step has no flow
leaving it. See
[Closing an instance that has nothing left to do](#closing-an-instance-that-has-nothing-left-to-do).

---

## Waiving, cancelling or holding one instance

One instance has to be dealt with outside what its process says: the approver
has left and the order cannot wait, the request was withdrawn, somebody has to
look before it goes further. An administrator of the instance's organization
does it through one route, in two calls: a preview, then an apply. It needs no
second version of the process, and nothing here is an `UPDATE`.

**A cancel and a hold are made by your apply. A waive is not.** Your apply
asks for it, and it is made when a different administrator of the organization
approves: see
[Approving a request for a second administrator](#approving-a-request-for-a-second-administrator).

What each act does and what it refuses is in
[Changing a process that is already running](process-change-in-flight.md#in-place-waive-cancel-and-hold).
The request and the reply, field by field, are in
[`integration.md`](integration.md#waiving-cancelling-or-holding-one-instance).

Three things hold for all of it:

- **A request changes nothing unless it says `"dry_run": false`.** Left out,
  it is a preview. Preview as often as you like: it holds no row and writes
  nothing.
- **Say why.** `reason` is required, at most 2,000 characters, and is kept in
  the instance's ledger and on its timeline with your name.
- **Your token has to be an administrator's, in the instance's organization.**
  If your account belongs to several organizations, add
  `-H "X-Organization-ID: <the instance's organization>"`. Without it the
  request is for your first membership, and an instance of another
  organization is answered 404, *no such process instance*.

### First, preview

The step's id is the `node_id` of the task that is waiting:

```sql
SELECT node_id, name, status, assignee FROM tasks
WHERE instance_id = '<instance-id>' AND deleted_at IS NULL
  AND status IN ('unclaimed', 'claimed', 'delegated');
```

```bash
curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "waive", "node_id": "<step-id>", "reason": "<why>"}' \
  "$METIS/api/v1/instances/<instance-id>/deviations" \
  | jq 'if .error then {error} else (.plan | {applicable, refusals, warnings, missing, visit_key, open_work_in_all,
                 open_work: [.open_work[] | {name, node_name, status, assignee}]}) end'
```

**A reply with `error` is a refusal, and its sentence is the answer.** Every
command here prints it when there is one: the request was malformed, the
account may not ask, or there is no such instance in the organization the
request is for. Nothing was read or changed beyond what the sentence says. The
one answer they cannot show is a missing or expired token: that is the plain
text `Unauthorized`, not JSON, so `jq` reports a parse error — sign in again.

Read it in this order:

1. **`applicable`.** `false` means an apply would be refused, and `refusals`
   says why: every reason at once, each a sentence that says what to do
   instead.
2. **`warnings`.** Whose work would be taken, and what the preview could not
   read. A warning does not stop an apply. A warning that something *was not
   read* is yours to go and read.
3. **`open_work`.** The tasks a waive or a cancel would withdraw, and who has
   them (a hold withdraws none, and lists what is waiting at the step). It
   lists the first 200; `open_work_in_all` is how many there are.
4. **`visit_key`.** Keep it. The apply sends it back.

### Waiving a step

For a step a person was to do — a user task or a manual task — that nobody
will now do. The task is withdrawn, its holder is told, and the instance moves
on as its process says. That happens when a second administrator approves, not
when you apply. Until then the task is open and its holder can still do it.

1. Preview, as above.
2. **If `missing` names anything, the waive has to say what it counts as.**
   Each name is a field of the step's form that something in the process
   decides from: a gateway, a decision table, a condition. Whether a waived
   approval counts as approved is the process owner's decision, not the
   operator's. Ask, then put the values in `outputs` and preview again:

   ```bash
   curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"kind": "waive", "node_id": "<step-id>", "reason": "<why>", "outputs": {"approved": true}}' \
     "$METIS/api/v1/instances/<instance-id>/deviations" \
     | jq 'if .error then {error} else (.plan | {applicable, refusals, warnings, missing, visit_key}) end'
   ```

   A value the instance already holds from an earlier visit to the step does
   not count; it has to be said again. `missing` is the complete list
   whenever the waive can be made: a waive that would need more than 50
   values, or one with a name over 255 characters, is refused for that. Only
   fields the step's form declares can be set.
3. **Read the warnings about what was not read.** A process a later step
   calls, and the process that started this one, are not read. Where the
   warning names one, open it and see what it does with the value before you
   apply.
4. Apply: the same request, with the `visit_key` and `"dry_run": false`. This
   asks for the waive. It does not make it.

   ```bash
   curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"kind": "waive", "node_id": "<step-id>", "reason": "<why>", "outputs": {"approved": true},
          "visit_key": "<visit_key>", "dry_run": false}' \
     "$METIS/api/v1/instances/<instance-id>/deviations" | jq '{applied, replayed, pending_approval, error}'
   ```

5. Give `pending_approval.request_id` to a second administrator. Nobody is
   told that a request waits, and there is no screen that lists them.

What comes back, and what to do with it:

| You get | It means | Do |
| :-- | :-- | :-- |
| 202, `"applied": false`, `pending_approval` | The waive was asked for. Nothing about the instance has changed. | Pass the `request_id` on. It waits until `expires_at`. |
| 202, `"applied": false, "replayed": true`, `pending_approval` | You asked for this already, and it still waits. No second request was made. | The same. |
| 200, `"applied": true, "replayed": true` | This same request was approved, and the step was waived then. Nothing was done twice. | Nothing. |
| 400 *A request to waive “…” is already waiting for approval (request …, asked by boss); approve or reject that one.* | Somebody else asked for a waive of this visit, or you asked for something else on it: another reason, other values. A second administrator does not get a request of their own by asking again. | Approve or reject the one that waits. To change what is asked, its requester withdraws it and asks again. |
| 400 *this instance has moved since you previewed it; preview again* | Between the preview and the apply a task of the step was completed, opened or withdrawn, or the instance left the step. | Preview again. If the instance no longer waits at the step, somebody did it, and there is nothing to waive. |
| 400 *this step was already waived by boss* | The visit has had its act, and this request asks for something else: another reason, other values. | Read the ledger, below. |
| 400 with the plan's refusals | The plan refuses now. | Preview again and read them. |
| 403 *asking for and giving a second administrator's approval needs an account, and this request carries none* | The token is an administrator's and names no account. | Ask with a token of your own account. |
| 500 | The server failed. Nothing was asked for, unless the failure came at the commit itself, where the reply cannot tell. | The sentence says what was being done; look in the log, then send the same request again: it asks, or it answers `replayed: true`. |
| No answer | The apply is waiting for the instance's lock. The server sets no deadline. | Stop the request and send the same one again. It asks, or it answers `replayed: true`. |

A gateway that has no branch for the value you gave is not found here. It is
found when the waive is made, and the approver is the one told: see the
refusals under
[Approving a request for a second administrator](#approving-a-request-for-a-second-administrator).

Once it is approved the task reads `canceled`, never `completed`, and the
ledger says `waive`. While it waits the ledger shows the same row with
`status: "pending_approval"`:

```bash
curl -sH "Authorization: Bearer $TOKEN" "$METIS/api/v1/instances/<instance-id>/deviations" \
  | jq 'if .error then {error} else (.deviations[] | {kind, origin, status, node_name, actor, approved_by, reason, created_at}) end'
```

A row that reads `rejected`, `expired` or `stale` is a waive that was asked
for and not made.

Do not read the instance's list of completed steps to tell a waived step from
a performed one: a waived step is in it. Read the task, the timeline or the
ledger.

**It will not waive:** work for a system or a worker (retry it, or resolve its
incident), a step that calls another process (waive the step inside that
process), a step with more than one way out, and a step that runs once and
that the instance reached twice at the same moment. Each refusal says which.

### Cancelling an instance

A cancel ends the **whole instance**, whichever step you name. Name a step it
waits at:

```bash
curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "cancel", "node_id": "<step-id>", "reason": "<why>"}' \
  "$METIS/api/v1/instances/<instance-id>/deviations" \
  | jq 'if .error then {error} else (.plan | {applicable, refusals, warnings, visit_key, open_work_in_all,
                 called_instances, called_instances_in_all}) end'
```

The preview lists every open task of the instance, on every branch, and its
warnings say what else goes: *2 piece(s) of work parked for outside workers
will be withdrawn.*, *1 open incident(s) on this instance will be closed.*
Apply with the `visit_key` and `"dry_run": false`, as for a waive. A cancel's
preview goes stale when anything open on the instance changes, so on a busy
instance expect to preview again.

Before you apply, know what a cancel does not do:

- **It does not recall a call already on its way.** A call to another system
  that is in flight when the instance is cancelled is still made. Its result
  is not written. If that call books or pays for something, undo it there.
- **It tells nobody the instance was cancelled.** Each holder is told their
  task was withdrawn. No event says the instance ended: an integration that
  watches the event stream or a webhook hears of the tasks and nothing else.
- **It leaves timers and queued calls as rows.** A pending timer does nothing
  when it comes due. A queued call is not made, and shows as pending on the
  cancelled instance until its turn comes.

Where one process calls another, cancel the called instance first:

- The caller is refused while a process it called has not ended: *This
  instance is waiting on 1 process(es) it started (…); cancel or finish those
  first.* The ids in the sentence are the instances to cancel or finish.
- The called instance can be cancelled where it waits. Its plan warns *This
  instance was started by another process (instance …), which is still
  waiting for it and is not resumed by this; cancel or hold that one next.*
  Cancelling it resumes nobody: the caller stays at its call step.
- Then cancel the caller, naming its call step, or hold it.

A called instance whose caller has already ended is cancelled the same way,
and its plan has no such warning.

For an instance with more than 200 open tasks, the cancel's ledger row names
the 200 tasks with the lowest ids and counts them all, and so does the row of
a waive of a step with more than 200 open runs. Who held a task it does not
name is on the task itself and in the notice they were sent:

```sql
SELECT id, node_id, assignee FROM tasks
WHERE instance_id = '<instance-id>' AND deleted_at IS NULL AND status = 'canceled' ORDER BY id;
```

### Closing an instance that has nothing left to do

An instance that is `active` and waits at no step. The query that finds them
is in [`upgrading.md`](upgrading.md#an-instance-a-migration-left-with-nothing-to-do).
A cancel that names **no step** is the supported way to close one:

```bash
curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "cancel", "reason": "<why>"}' \
  "$METIS/api/v1/instances/<instance-id>/deviations" \
  | jq 'if .error then {error} else (.plan | {applicable, refusals, warnings, visit_key}) end'
```

- The plan warns *This instance is not waiting at any step. Cancelling it
  closes it.* That says where the instance waits and nothing more. Whether a
  timer or a message could still move it was not looked at: check its jobs
  and what it is waiting for with the queries under
  [A stuck process instance](#a-stuck-process-instance) before you close it.
- *“…” is still open though the instance is not waiting there; it will be
  withdrawn.* means a task was left open with nothing under it. The cancel
  withdraws it and tells its holder.
- *This instance was started by another process (instance …), which is still
  waiting for it and is not resumed by this; cancel or hold that one next.*
  The instance was called by another, which waits for it at its call step and
  will go on waiting. Closing this one resumes nobody. Cancel the caller next,
  naming its call step, or hold it.
- Refused with *This instance is waiting at “…”; say which of those steps it
  is to be ended at.*: it is not one of these. It waits somewhere. Name the
  step.

Apply with the `visit_key` and `"dry_run": false`. The ledger row and the
timeline entry name no step: *This instance was ended by boss while it was not
waiting at any step.*

A waive and a hold need a step, so an instance with nothing left can be
cancelled and cannot be held.

### Holding an instance, and letting it go

A hold raises an incident at the step the instance waits at, so that it shows
where somebody will look. **It does not stop the step's work.** Whoever holds
the task can still complete it, and the instance then moves on with the
incident still open.

```bash
curl -sH "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "hold", "node_id": "<step-id>", "reason": "<why>"}' \
  "$METIS/api/v1/instances/<instance-id>/deviations" \
  | jq 'if .error then {error} else (.plan | {applicable, refusals, warnings, visit_key}) end'
```

Apply with the `visit_key` and `"dry_run": false`.

- **The reason is public within the organization.** It is written into the
  incident's text, *held at “Operations approve” by boss: …*, which anyone who
  can read the instance's incidents reads.
- **The inbox words it as a failure.** The incident inbox shows it as
  *Operations approve failed*, with an explanation written for failed calls
  and a **Try again** button. It is a hold. Read the incident's own text.
- **A step that already has an open incident keeps it.** The plan warns
  *“Operations approve” already has an open incident; the hold will use it.*
  The hold is recorded, and no second incident is raised. On a step that calls
  a system or parks work for a worker, that incident may be the engine's own
  failure, and **Try again** on it retries the call.

**To let a hold go**, resolve its incident. That takes the operator role, and
it is what **Try again** does:

```bash
curl -sH "Authorization: Bearer $TOKEN" \
  "$METIS/api/v1/incidents/<instance-id>" \
  | jq 'if .error then {error} else (.incidents[]? | {id, step: .node.id, error, status}) end'
curl -sX POST -H "Authorization: Bearer $TOKEN" "$METIS/api/v1/incidents/<incident-id>/resolve"
```

**Nothing records who let a hold go, or when they decided.** Resolving writes
no timeline entry and no ledger row. The incident's `status` and `resolved_at`
are all there is, and the ledger's `hold` row goes on reading `open`. If it
matters who released it, write that down somewhere that is kept.

After the incident is resolved the step can be held again: preview again and
apply. The earlier preview's `visit_key` will not do; sent again it answers
`replayed: true` for the first hold and holds nothing.

If the held step is completed and the instance finishes, the incident stays
open until somebody resolves it. A cancel of a held instance closes the hold's
incident with the instance's other open incidents.

---

## Approving a request for a second administrator

A waive of a step, and a migration that skips a step or loosens a rule on work
still running, are not made by the administrator who asks. Their apply is
answered 202 with a `pending_approval`, and nothing changes until a
**different** administrator of the organization approves. What asks, and what
an approval does, is in
[Changing a process that is already running](process-change-in-flight.md#a-second-administrator).
The routes, field by field, are in
[`integration.md`](integration.md#requests-for-a-second-administrator).

Know these before you approve anything:

- **You are the check.** The server checks that your account is not the one
  that asked. It does not review the request for you, and for a migration it
  does not review the difference between the two versions. Read
  [What it does not review](process-change-in-flight.md#what-it-does-not-review).
- **There is no screen and no notice.** Requests are listed, read and decided
  through the API. Nobody is told that one waits: the requester gives you its
  id, or you list them.
- **Your token has to be an administrator's, in the request's organization**,
  with `X-Organization-ID` if your account belongs to several. Another
  organization's request is a 404, *no such request*, as one that does not
  exist is.
- **A request expires.** 72 hours after it was made unless
  `METIS_DEVIATION_APPROVAL_TTL` says otherwise. `expires_at` is on the
  request.

### What waits

```bash
curl -sH "Authorization: Bearer $TOKEN" "$METIS/api/v1/deviation-requests" \
  | jq 'if .error then {error} else {total, requests: [.requests[] | {id, kind, requested_by, reason, expires_at}]} end'
```

With no `status` it lists what still waits, newest first, 50 to a page (at
most 200 with `page_size`). `?status=` takes one of `pending_approval`,
`approved`, `applied`, `interrupted`, `stale`, `rejected`, `expired`, and
`?project_id=` narrows it to one project. No call lists every status at once.

**A listed request is not the whole request.** The list leaves out what was
asked, the plan and the instances. Read the one you are asked to approve:

```bash
curl -sH "Authorization: Bearer $TOKEN" "$METIS/api/v1/deviation-requests/<request-id>" \
  | jq 'if .error then {error} else (.request | {kind, status, requested_by, reason, because, expires_at,
        instance_id, source_definition_id, target_definition_id, instances_in_all, command, plan}) end'
```

Read it in this order:

1. **`because`.** Why this needs you, one sentence for each reason. For a
   migration it lists the first ten and counts the rest.
2. **`command`.** Exactly what will be carried out if you approve. You cannot
   change it: an approval carries a note and nothing else.
3. **`plan`.** The plan **as the requester was shown it**, when they asked.
   It is not made again for you to read. Every list in it that is cut short
   has its count beside it.
4. **`instances`, `instances_in_all`** (a migration). The instances that had
   not ended when it was asked for. It lists the first 200. The approval
   covers all of them and no other.

**To see a waive's plan as the instance stands now**, send the request's
`command` as a preview yourself: `kind`, `node_id`, `reason` and `outputs`,
with no `dry_run`. Its `refusals` will include *A request to waive “…” is
already waiting for approval …*, because one is; everything else in the plan is
current. **For a migration**, send the stored `command` to the migrate route
as a dry run.

Three things a request's wording can hide:

- **A migration's decision applies to every listed instance waiting at the
  step when the migration runs, not only those waiting there when it was
  asked for.** "1 waits there now" was true when it was asked.
- **A request that reads `pending_approval` may no longer hold.** Nothing
  watches a waiting request. If the step was completed, or the instance was
  cancelled or migrated, the request still reads as waiting until somebody
  tries to approve it or it expires.
- **`self_approved: true`** on a decided request means the requester approved
  it themselves, in an organization named as having one administrator.

### Approving

```bash
curl -sX POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason": "<a note, optional>"}' \
  "$METIS/api/v1/deviation-requests/<request-id>/approve" \
  | jq 'if .error then {error} else {applied, status: .request.status, passed_over_in_all, passed_over} end'
```

**Approving a waive makes the waive, in that call.** The task is withdrawn,
its holder is told, and the instance moves on.

**Approving a migration runs the migration, in that call.** The call lasts as
long as the run does, and the server sets no deadline on it. Use a client that
waits. A client that gives up and closes the connection may stop the run
part-way (read from how a request's cancellation reaches the run, not run);
what it had moved stays moved, and the request reads `interrupted`.

| You get | It means | Do |
| :-- | :-- | :-- |
| 200, `"applied": true`, request `applied` | It was carried out. For a migration, `passed_over` lists up to 200 instances the run left alone, each with why, and `passed_over_in_all` how many. | For a migration that passed instances over: the requester asks again for those. This reply is the only place they are listed. |
| 200, `"applied": false`, request `applied` | A migration's run ended without failing and changed nothing: every instance was passed over, or none was active. The request is spent. | Read `passed_over`. The requester asks again if it is still needed. |
| 403 *You asked for this. A different administrator has to approve it.* | It is your own request. | Somebody else approves it, or you withdraw it. |
| 403 *You asked for this, and nobody else administers this organization, so it waits. …* | It is your own request and you are the organization's only administrator. | See [An organization with one administrator](#an-organization-with-one-administrator). |
| 400 *The administrator who asked for this, boss, no longer administers this organization, so nothing was applied. It has to be asked for afresh by somebody who does.* | Whoever asked has since been deleted, taken out of the organization or lost the administrator role in it. A waive or a migration. The request now reads `stale`, and `outcome.why` says which of the three. | Somebody who administers the organization previews and asks again. If the role was taken away by mistake, give it back first; the old request stays closed either way. |
| 400 *This request no longer holds — the instance has moved since it was asked for — so nothing was applied. Preview again and ask afresh.* | A waive: the step was completed, a task of it changed, or the instance left the step or ended. The request now reads `stale`. | Tell the requester. If the step still needs waiving, they preview and ask again. |
| 400 *This request no longer holds, so nothing was applied: … Ask again.* | A migration: the plan now refuses, an instance reached the version after it was asked for, or the migration can no longer be planned. The sentence says which. The request now reads `stale`. | Tell the requester. They preview and apply again, which asks afresh. |
| 400 *This request expired on … before anybody approved it, so nothing was applied. Ask again if it is still needed.* | Its deadline passed. The request now reads `expired`. | The requester asks again. |
| 400 *boss approved this on …, and it was applied.* and its neighbours | Somebody decided it first. The sentence says who, when and what became of it. | Nothing. |
| 400 *this instance is suspended, and a suspended instance is not waived, cancelled or held in place* | A waive of a suspended instance. Nothing changed and the request still waits. Nothing in the product suspends an instance or resumes one, so this is a row changed outside it. | Reject the request, or leave it to expire. |
| 400 *The values given fit no way out of “Large order?”… The request is still waiting: reject it, and the waive can be asked for again.* | A waive: a gateway has no branch for the value the requester gave. Nothing changed and the request still waits. You cannot change the values. | Reject it with that as the reason. The requester previews again and gives a value a branch accepts. |
| 400 *invalid argument: the approved migration did not finish: …* | A migration: the request was approved, and the run's own plan then refused, because an instance moved on in between to where the migration cannot take it. Nothing was moved. The request reads `interrupted`, and its `outcome.error` says *the run was refused before it moved anything: the plan made when it came to start refused the migration*. The log says it as a warning, *An approved migration was refused before it moved anything…*, with the plan's own words. | The requester previews again; the plan now says what it needs. |
| 403 *forbidden: the approved migration did not finish: …* | A migration: approved, and refused at the gate before anything was moved, most often because an instance reached the version between the approval and the run. The request reads `interrupted`. | The requester asks again. |
| 500 *the approved migration did not finish: … what its run had done stands, and what remains has to be asked for again* | A migration's run stopped part-way. Some instances were moved. The request reads `interrupted`. | Read the request: `outcome.changed` is how many it acted on, and `outcome.error` one sentence. The cause in its own words is in the server's log, under *An approved migration stopped part-way.* Then the requester previews again and asks for what remains. |
| 500, anything else | The server failed before or while approving. For a waive nothing changed and the request still waits. | Look in the log, then approve again. |

An approval is safe to send again. A second one is answered with the 400 that
says who approved (`TestAnApprovalSentAgainIsAnsweredOnce`).

After a part-way failure the request says how far the run got:

```bash
curl -sH "Authorization: Bearer $TOKEN" "$METIS/api/v1/deviation-requests/<request-id>" \
  | jq 'if .error then {error} else (.request | {status, decided_by, decided_at, outcome}) end'
```

`outcome.count_unknown: true`, with no `changed`, means the run stopped on a
panic and nobody knows how far it got. Plan the migration again to see which
instances are still on the old version.

### Rejecting, and withdrawing

A rejection needs a reason. Any administrator of the organization may reject.
**Sent by whoever asked, the same call is a withdrawal**; there is no route of
its own for one.

```bash
curl -sX POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason": "<why>"}' \
  "$METIS/api/v1/deviation-requests/<request-id>/reject" \
  | jq 'if .error then {error} else (.request | {status, decided_by, decision_reason}) end'
```

Nothing about any instance changes. The same thing can be asked for again
afterwards. For a waive the instance's timeline says who rejected it, or that
its requester withdrew it, and why. **For a migration nothing is written to
any instance's timeline**: the request itself is the record.

- 400 *Say why: a rejection keeps its reason with the record.* — no reason.
- 400 *This request already expired on ….* — its deadline had passed. It now
  reads `expired`.
- 400 *boss approved this on …; it is being applied.* — a migration that is
  running cannot be rejected.

### What became of a request

| It reads | It means | What to do |
| :-- | :-- | :-- |
| `pending_approval` | It waits. | Approve or reject it before `expires_at`. |
| `approved` | A migration, approved, whose run is going. | Wait. It does not rest here. |
| `applied` | Carried out. For a migration, `outcome.changed` and `outcome.passed_over` count what the run did; `outcome.note` says so when it changed nothing. | Nothing, or ask again for what was passed over. |
| `interrupted` | A migration, approved, whose run stopped part-way, was refused before it began, or did not report back within its window (its deadline, or an hour after the approval, whichever came first). What it had done stands. | Plan again and ask for what remains. A request narrowed with `instances` must leave out the ones already moved. |
| `rejected` | Somebody said no, or its requester withdrew it. `decided_by` and `decision_reason` say who and why. | Ask again if it is still needed. |
| `expired` | Nobody decided it in time. | Ask again if it is still needed. |
| `stale` | Somebody tried to approve it and it no longer held, or whoever asked for it no longer administers the organization. `outcome.why` says why and `outcome.attempted_by` who found it. | Preview again and ask afresh — by somebody who administers the organization. |

A request past its deadline reads `expired` at once. A pass every ten minutes,
on every replica, writes that down, and the server's log then says *Recorded
the expiry of requests waiting for a second administrator that nobody decided
in time*. A warning that begins *Recorded that approved requests for a second
administrator were interrupted* means an approved migration's run never
reported back: a server stopped mid-run, or a run outlived its hour. Find the
request under `?status=interrupted` and plan the migration again.

### An organization with one administrator

With no setting, a sole administrator's waive or skip waits. Their own
approval is refused, and the request expires at its deadline. They can:

- have a second **person's** account made an administrator of the organization
  (`PUT /api/v1/users/{id}/organization-roles`), who then approves;
- withdraw the request and cancel or hold instead, which need nobody else;
- let it expire.

Whoever operates the installation can instead name the organization as one
with a single administrator:

```
METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS=<organization-id>[,<organization-id>…]
```

- **By id, comma-separated.** Spaces around an entry are dropped and an id
  given twice counts once. There is no value that turns it on everywhere:
  `true` is an entry that names no organization.
- **Read once, when the server starts.** Changing it takes a restart of every
  replica. No request, header or organization setting can add an organization.
- **One name.** The pre-rename `GOBPM_` spelling is not read for this setting.

**What it permits.** In a named organization, an administrator may approve a
request they asked for themselves, while no other account that is not deleted,
belongs to the organization and holds the Administrator role exists. They
must give a reason. The record says nobody else approved it: the request reads
`self_approved: true`, and the ledger and the timeline carry
`self_approved`, `other_administrators: 0` and `organization_id`. In an
organization that is not named, and in a named one that has a second
administrator, the requester is refused as before.

**What it does not close.** In a named organization, an administrator who can
change roles can take another administrator's role away, approve their own
request, and give the role back. Each change of roles is in the server's log
with who made it, and nowhere else
([below](#who-changed-who-administers)). **Name an organization only while it
truly has one administrator, and take it off the list once it has a second.**

**What the server says at start-up**, each a warning with
`setting: METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS`:

| Line | Means |
| :-- | :-- |
| *In each organization this setting names, an administrator may approve their own request for a second administrator while nobody else administers that organization. …* with `count` and `organizations` | The exception is on for those organizations. Said at every start while any is named. Every organization is listed: a line lists fifty, and a list longer than that goes on in further warnings, *More of the organizations this setting names: …*, each with `count` (of them all) and `listed_from` (where in the list it goes on from). |
| *entry 2 of METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS, "…", is not an organization id, so it names no organization and is ignored* | That entry does nothing. The entries beside it still apply. |
| *These ids name no organization of this installation, so naming them does nothing. Check them against the organizations' ids and take them off the list.* | An id that is well formed and is nobody's: mistyped, or another installation's. Every such id is listed, fifty to a line, the rest under *More ids this setting lists that name no organization of this installation.* |
| *Could not check that the ids this setting lists name organizations of this installation.* | The check failed. The list means what it meant. |

With the setting unset or empty the server says nothing about it.

**While it runs**, also warnings:

- *An administrator approved their own request for a second administrator,
  which this setting allows in an organization it names while nobody else
  administers it. No second person approved it; the ledger and the trail say
  so.* (`organization`, `request`, `actor`, `actor_id`.) The exception was
  used.
- *This setting names this organization as having one administrator, and it
  has another: an administrator's approval of their own request was refused.
  Take the organization off the list.* (`organization`, `request`.) **The
  list is out of date.** Act on it: a list left that way is the exception
  waiting for the day one of the two removes the other.
- *An administrator tried to approve their own request for a second
  administrator and was refused. Nothing changed, and nothing else records the
  attempt.* (`request`, `actor`, `actor_id`.) Written with or without the
  setting.

### Who changed who administers

The second approver rests on two administrators being two people, and an
administrator can change who the administrators are. These five lines in the
server's log, at info level, are the only record of that, so keep the log:

| Line | Written when |
| :-- | :-- |
| *An account was created.* | an account is created, whatever roles it holds — by the account service, or by set-up, which creates the installation's first administrator |
| *An account's roles were changed.* | the roles an account holds in every organization change |
| *An account's roles in an organization were changed.* | the roles it holds in one organization change |
| *An account was deleted.* | an account is deleted |
| *An account's password was set from the server's command line.* | `--reset-password` sets a local account's password. It changes no role: it is here because it is the one way into an account that needs no session |

Each carries `actor` and `actor_id` (who did it; both empty where nobody was
signed in), `target` and `target_id` (whose account), `organization` (the
organization the request was for, when it was for one), `roles_before` and
`roles_after` (held in every organization), and `organization_roles_before`
and `organization_roles_after` (held in one organization each, by organization
id). The lines of an account created and of one deleted also carry
`member_of`, the organizations the account belongs to: a role held on the
account is held in each of them. The two changes nobody signed in can have
made carry `made_through` — `set-up` for the first administrator, and
`--reset-password, run on the server` for a password — in place of an actor.
`actor` and `target` are usernames, and a username can be an email address:
an account an identity provider signs in is named by the address when the
provider gives no other name.

A change that was refused writes no line, and nor does one that changes
nothing: roles set to what they already are, in whatever order or case.

To see who administered an organization around an approval, search for the
organization's id and the approver's and the requester's account ids in the
hour either side of the request's `decided_at`:

```bash
kubectl -n metis logs deploy/metis --since=24h \
  | grep -E 'An account was (created|deleted)|An account.s (roles|password)' \
  | grep -E '<organization-id>|<account-id>'
```

What these lines do not cover: a sign-in through the identity provider that
changes which organizations an account belongs to, and a rename of an account
that changes no role (a rename made together with a change of roles is said
under the new name and the same `target_id`). There is no table to query for
any of it.

### A request the pass cannot close

A last resort, for a request whose stored rows are damaged: its ledger row is
gone or can no longer be read, after the loss of an encryption key or
corruption. You find out from the log, on every pass, on every replica:

- a warning naming it: *A request the clock has closed could not be written
  down as closed, and was left as it was. It reads as closed and nobody can
  act on it, but its row still says otherwise and it still holds what it was
  asked for.* (`request`, `error`; the first five of a pass);
- a warning counting them: *A pass over the requests past their deadline left
  some as they were; the next pass meets them again* (`closed`, `not_closed`,
  `held`, `budget_spent`, `held_limit_reached`, and `stopped_by` when
  something stopped the pass);
- and *Could not record everything the clock has decided about requests for a
  second administrator; …*.

**What state it is in.** It reads `expired` to every reader and cannot be
approved. Nothing was waived. But its waiting ledger row still holds the
visit, so that step of that instance cannot be asked to be waived again. The
step's work can still be done by whoever holds it, and the instance can still
be cancelled or held. The requests behind it are closed in the same pass. A
request left only because another transaction held its ledger row is closed
by the next pass, and is not this case: the pass counts it as `held`, not as
`not_closed`, and does not name it in the first warning above.

**How long a pass can take.** A pass makes at most ten thousand attempts, each
a short transaction, and waits at most two seconds for a row somebody holds.
It ends once it has waited out thirty such rows — a minute for each of its two
reads, so two minutes at the worst — and says so (`held_limit_reached`); the
next pass, ten minutes on, goes on. The rest of the retention pass, the
re-offer of external tasks in each environment's database among it, waits
behind the sweep for no longer than that.

Rejecting it over the API does not help: a rejection closes the same ledger
row and fails the same way. (A request whose own stored plan no longer opens
is a different case, and needs none of this: it can be read, with
`unavailable` naming what could not be, and rejected, withdrawn and expired as
any other. Only approving it is refused.) Read both rows:

```sql
SELECT id, kind, status, live_key, instance_id, requested_by, expires_at
FROM deviation_requests WHERE id = '<request-id>';

SELECT id, status, live_visit_key, decided_at
FROM instance_deviations WHERE request_id = '<request-id>';
```

Only for a request that reads `pending_approval` there and is past its
`expires_at`, close both in one transaction: the request first, as every
decision takes it first, and then the ledger row that waits on it, dated by
the deadline as the pass would have dated it.

```sql
BEGIN;
UPDATE deviation_requests
   SET status = 'expired', live_key = NULL, updated_at = now()
 WHERE id = '<request-id>' AND status = 'pending_approval' AND expires_at <= now();
-- must report UPDATE 1; on UPDATE 0, ROLLBACK and stop
UPDATE instance_deviations d
   SET status = 'expired', live_visit_key = NULL, decided_at = r.expires_at
  FROM deviation_requests r
 WHERE r.id = '<request-id>' AND r.status = 'expired'
   AND d.request_id = r.id AND d.status = 'pending_approval';
COMMIT;
```

The first statement is the guard. It reports `UPDATE 1` only for a request
that still waits and is past its deadline; for one that is not yet due, or
that somebody decided meanwhile, it reports `UPDATE 0` — then `ROLLBACK` and
stop, because there is nothing to repair. The second statement closes the
ledger row only of a request the first has just closed, so run alone, or
after an `UPDATE 0`, it changes nothing either. A request for a migration has
no ledger row: for one, the second statement reports `UPDATE 0`, which is
right.

This writes no entry on the instance's timeline, which the pass would have
written. Record what you did somewhere that is kept.

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

## Somebody signing in through the identity provider is refused

Shows up as a person who signed in at your identity provider getting **403**
from every page, with an error that names a setting or a claim, and as a
warning in the log:

```
Refused a sign-in through the identity provider: it does not place the person in any organization here
```

Its `reason` field says which case it is. They are authenticated; nothing
places them in an organization here, so no account was created for them.

- **"an operator has to set METIS_OIDC_ORGANIZATION_CLAIM"** — the setting is
  unset, and every OIDC sign-in is refused; boot also warned. Set it to the
  name of the ID-token claim that lists people's organizations, and restart.
- **"has no \"<claim>\" claim"** — the provider does not send that claim, or
  sends it under another name. Decode one of their ID tokens (the payload is
  base64 JSON) and compare. Add the claim at the provider, or correct the
  setting.
- **"none of the organizations named in the \"<claim>\" claim … exists here"**
  — the claim is there but no value is the id of an organization here. The
  values must be ids, not names:

  ```sql
  SELECT id, name FROM organizations WHERE deleted_at IS NULL ORDER BY name;
  ```

A fix at the provider needs no restart here. The person signs in at the
provider again, to get a token that carries the corrected claim, and that token
is placed afresh; the old one stays refused until it expires.
[`integration.md`](integration.md#signing-in-with-oidc) has what the claim must
hold, and what a first sign-in creates.

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

**Rolling back past migration 34 takes the second administrator away, and
strands what waits for one.** The release before it applies a waive and a
migration's skip on one administrator's call, and has no way to approve,
reject or expire a request. Reject every request that still waits, or let it
expire, before you roll back:
[Upgrading](upgrading.md#a-second-administrator-approves-waivers-and-skips-migration-34)
has the query and what the older release does with one that is left.

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
- **A control a release adds is in force only on the pods that have it.** While
  a canary of the release that adds the second administrator runs beside the
  one before it, a waive or a migration's skip served by a stable pod is
  applied on one administrator's call. Which pod serves a request is not the
  administrator's to choose, and not yours to rely on. Keep that canary short;
  [Upgrading](upgrading.md#a-second-administrator-approves-waivers-and-skips-migration-34)
  has the query that finds what was applied that way.
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
