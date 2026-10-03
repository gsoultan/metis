# Changing a process that is already running

Management removes the operations manager's approval from the quotation process. Three
hundred quotations are already in flight, and thirty of them are sitting in that person's
inbox right now. What happens to them?

This document is the analysis and the answer. It covers what breaks, what is safe, when
changing in flight is the *right* call, and what the engine now does about each.

> **The one sentence.** Migration is right when the in-flight population is *already*
> broken or *already* costing money. Drain is right when it is healthy. Every failure mode
> in §2 is "moving introduced a risk that waiting wouldn't have"; every case in §3 is
> "waiting costs more than moving".

---

## 0. First, the thing most teams get wrong

Scheduling a release for next Monday does **not** mean the operations manager stops
approving on Monday.

A quotation submitted Sunday night starts on v1. It reaches the operations step on
*Tuesday* — two days after the cutover. The population parked on a removed node keeps
growing after the cutover date. The real horizon is:

```
cutover + the longest time for an already-started instance to reach that node
```

So separate the two intents before anything else, because they need different mechanisms:

| What management means | Mechanism | Status |
| :-- | :-- | :-- |
| "New quotations follow the new rules from Monday" | The release timeline alone | **Works today.** `ScheduleRelease`, `ProcessDefinitionReleaseModel` |
| "Nobody performs an operations approval after Monday" | Release timeline **+ a migration of the stragglers** | Needs the migration in §5 |

The release timeline is a pure function of `(rows, now)` — the newest row whose
`activate_at` has passed. No scheduler wakes up, nothing is missed because a replica was
restarting, every replica agrees without coordinating. And it says nothing about running
instances: those pin their definition by ID and drain on the version they started on. That
is also the correct legal default — the rules in force when a quotation was submitted
generally govern that quotation.

---

## 1. The three populations

A single removal is not one question. It is three, and only one of them is hard.

| Population | Where the token is | Answer |
| :-- | :-- | :-- |
| **A — already past** | on `salesApprove` | Migrate. The node exists in both versions. Nothing to decide. |
| **B — parked on the removed node** | in the operations manager's inbox | **The business decision.** |
| **C — not yet arrived** | on `supervisorReview`, or not started | Migrate. They should never see the removed step. |

Population B has no technically correct answer, only a business one:

- **B1 — honour it.** The change is policy going forward; approvals already requested get
  finished. → let those instances drain on v1. *This is the default and it is free.*
- **B2 — deem it granted.** The role was eliminated; pending approvals are moot. → a `skip`
  action: the engine advances past the step and records that nobody performed it.
- **B3 — void it.** The step was a mistake; the pending approval is invalid. → a `cancel`
  action: the instance ends where it stands.

No engine can choose for you. What an engine owes you is the ability to *express* the
choice, apply it to only the instances it should apply to, and leave a trail an auditor can
read. All three are now expressible — see §5.

---

## 2. Negative cases — what breaks

### Class A: silent corruption (worst — no error, wrong outcome)

`ProcessInstance` carries **four** structures identified by node ID:

```go
CompletedNodes   []string                       // the "already done" guard
CompensatedNodes []string
MultiInstance    map[string]MultiInstanceState  // progress through a per-item node
Joins            map[string]int                 // branches arrived at each waiting gateway
```

Before this work, `apply()` wrote **exactly three row types** — the instance's definition ID
and token node IDs, `task.NodeID`, and the job's node and definition ID. Which generalises
to a rule worth keeping:

> **Anything keyed by node ID that is not a token, a task or a job was, by construction,
> left pointing at the old graph.**

Three consequences, in severity order. All three bite only when a mapping **renames** a node
(`from != to`) — which is exactly what `opsApprove → salesApprove` is.

| Case | What happened | Now |
| :-- | :-- | :-- |
| **Renamed join gateway** | `Joins["opsJoin"]` orphaned, `Joins["newJoin"]` reads 0. The gateway waits forever for a branch that already arrived. No error — the instance just never finishes. | Rekeyed. `TestMigrationRekeysJoinCountersSoTheJoinStillCompletes` |
| **Multi-instance mid-collection** | `IsMultiInstanceActive` finds nothing under the new ID and **re-enters the node** — everyone is asked again, and the approvals already given appear twice. | Rekeyed |
| **Completed-node guard stranded** | `IsCompleted` loses its record, so an activity can run a second time. For a service task that charges a card, that is the double-firing incident `AGENTS.md` §0 names. | Rekeyed; unmapped entries kept, because a step the new version deleted is still a step this instance performed |

Two mappings cannot merge onto one target if both sides carry a counter: adding two arrival
counts together invents branches that never arrived, and dropping one loses a branch that
did. That is refused (`TestMigrationRefusesToMergeTwoCounters`).

### Class B: the data contract — the one everyone forgets

**A removed user task is a removed *data producer*, not just a removed step.**

The operations manager does not only approve. They set `riskTier`, `approvedCreditLimit`,
`marginOverrideReason`. Downstream, a gateway branches on `riskTier` and the sales manager's
form pre-fills from `approvedCreditLimit`.

Delete the node and those variables are never written. At the gateway, no condition matches.

Metis behaves **correctly and loudly** here — §0 forbids falling back to `flows[0]`, so it
raises an incident. But understand the operational shape: migrate 300 instances at 2am and
you get **300 simultaneous incidents at the same gateway**, from a change nobody connected
to gateway logic.

**The worse variant:** if that gateway has a **default flow**, there is no incident. Every
instance silently takes the default. High-risk quotations route as low-risk, and you find
out at quarter-end.

The plan now walks the target graph forward from every landing node and warns about exactly
this. It also lists `RemovedNodes` whether or not anything sits on one, because that is the
first thing a reviewer needs.

### Class C: structural attachment

- **Boundary events could be detached.** A three-day escalation on `approve` is not a
  three-day escalation on whatever else the target attaches one to. The planner checked only
  that target nodes *exist*. Camunda 8 refuses exactly this shape; Metis now does too
  (`boundaryRefusals`).
- **Message subscriptions** were the fourth row keyed by node ID, and `apply` wrote three.
  A renamed catch event left its subscription naming a node the new version does not have:
  the message arrives, correlates to nothing, and the sender is never told. It is also the
  one kind of waiting that does not sit under a token — a message boundary event keeps its
  subscription on the boundary node while the token stays on the task it guards — so the
  token checks could not stand in for it. Now surveyed for landing and re-pointed with the
  work; a cancelled instance closes the ones it held.

### Class D: concurrency

The operations manager has the form open and clicks Submit at the instant migration runs.
`CompleteTask` took the instance lock *after* deciding who was allowed to complete, so the
decision was made against a row another transaction was free to rewrite before the write
landed. It now authorises, takes the lock, then **re-reads and re-checks** — and migration
takes the same lock for the whole rewrite, so the two serialise instead of both acting on
what they found.

This one is argued rather than demonstrated: the window is a few statements wide and the
test in `race_test.go` does not reliably land inside it (it passes against the old ordering
too, which its comment says). What that test does guard is the invariant — however the two
interleave, the result is one of the two legitimate orders and never a mixture.

A decision has the same window, and a wider one. A migration lists the instances once and
then takes them one at a time, so the copy it holds of the last instance is as old as the
whole run. Whether an instance was waiting at a step being skipped, cancelled at or held at
was asked of that copy and never again once the instance was locked: the operations manager
who clicked Submit in between had the step advanced past a second time — two tokens and two
open tasks on the step after it — and the trail, and now the ledger, said the approval was
waived. Each decision asks again under the lock (see *Node actions*, below), and this one is
demonstrated: `moved_on_test.go` puts the completion between the listing and the lock on
purpose, for a skip, a cancel and a hold.

What is still not asked again is where a *moved* instance's work lands. An instance that
advances between the listing and its lock onto a step the plan did not find it on is
rewritten as it then stands; if that step is one the new version does not have — the step a
skip was meant to clear, reached a moment too late to be skipped — the instance arrives on
the new version with a token the new graph cannot place. The planner's "must land somewhere"
check ran on the listing. Open, and written down in the roadmap. Until it is closed the
window is as long as the run takes to reach the instance, so it is narrowest for a migration
that names a few instances at a time when the steps before a removed one are quiet.

This is also why `Claimed` and `Delegated` count separately in the plan: an unclaimed task
is a queue item, a claimed one is a person mid-sentence.

### Class E: control and compliance

- **Segregation of duties collapses quietly.** The operations approval may exist so that no
  single reporting line approves its own deal. Remove it and in some org paths the
  submitter's own manager becomes the only approver. Auditors look for evidence that no
  single role can complete the transaction loop. A node can now declare
  `separation_of_duties` — the steps whose performer may not also perform it — and the
  engine refuses the second one at claim *and* at completion. Removing a step a surviving
  rule names is a **warning**: the rule survives the edit and stops meaning anything,
  because it is satisfied by everybody once nobody has performed the step it names.
- **Delegation-of-authority thresholds.** DoA matrices are value-banded. Remove the step that
  caught everything under $50k and those quotations now need an approval the matrix does not
  authorise — or skip approval entirely.
- **Retroactive legal effect.** Migrating a Friday quotation onto Monday's rules applies
  terms that did not exist when the customer was quoted.
- **Conformance checking breaks.** A migrated instance is a trace no single model explains.
  If the log cannot distinguish *migrated* from *deviant*, compliance analysis is noise.
  Fixed by the audit event: `instance_migrated`.

### Class F: blast radius and reversibility

- **Shared sub-processes.** An approval invoked by twelve parents via call activity is twelve
  processes, not one.
- **You cannot un-skip.** Release rollback works (`TestPromotingAnOlderVersionRollsBack`).
  Instance migration is not symmetric: a cancelled approval task cannot be resurrected.

---

## 3. Positive cases — when migrating is right

These are real and underweighted. In each, **not** migrating is the larger risk.

1. **The dead queue (strongest).** The approver resigns; the assignment resolves to an empty
   group. Three hundred quotations are frozen forever — drain never completes. Removing the
   node is the only way to release real revenue. This is recovery, not risk-taking.
2. **A broken assignment or condition.** Same shape, bug instead of attrition.
3. **Regulatory stop.** A step becomes *illegal*. "We will keep breaking the rule for the 400
   instances already running" is not a position you can hold. Drain is wrong by definition.
4. **Compromise response.** An approver's account is compromised; every pending approval by
   them is suspect and must be re-routed now.
5. **Scale economics.** 5,000 in-flight loans × two days saved. Drain would take months.
6. **Continuity.** A wet-signature step becomes impossible.
7. **SLA rescue.** A bottleneck approval breaching contractual penalties.
8. **Compounding modelling error.** Every day of drain adds wrong outcomes to remediate.

Cases 1–4 are *the population is already broken*. Cases 5–8 are *waiting has an accruing
cost*. **If your case fits neither, drain.**

---

## 4. The triage

Four questions, in order. Stop at the first that decides.

1. **Is the in-flight population healthy?** Can every parked instance still progress by
   itself? If **no** → §3, migrate. The engine risk is bounded; the alternative is permanent.
2. **Does the removed node produce data later steps consume?** Check `RemovedNodes` and the
   default-flow warnings in the plan. A downstream gateway with a default flow is a *silent*
   divergence, not a loud one.
3. **Does the node carry a control obligation?** Mark it `compliance_relevant` and the plan
   will hold rather than let it pass unremarked.
4. **Can you wait?** Compare the **drain horizon** from §0 against the business deadline. If
   it fits, drain and build nothing.

---

## 5. What the engine does now

### Refusals (block the apply)

| Refusal | Why |
| :-- | :-- |
| Work parked where the target has no node | The original check; a stranded token cannot be un-stranded |
| Engine bookkeeping with nowhere to land | Names the counter, not just the token riding on it |
| Two counters merging onto one node | No correct way to add two arrival counts together |
| A boundary event moved off its activity | A timer firing against work that is not running |
| A mapping naming a node the target lacks | A typo, wrong regardless of what is running |
| Different process keys or projects | Not a version change |
| **An unacknowledged control obligation** | See below |

### Warnings (do not block)

- A downstream gateway with a **default flow**, which would route silently instead of raising
  an incident when a variable the removed step used to set is missing.
- Tasks **claimed or delegated right now**, which go back to the queue.

Warnings are kept separate from refusals deliberately: a warning dressed as an error teaches
people to click past the list, and then a real refusal gets clicked past too.

### Compliance holds

A node marked `compliance_relevant` (with an optional `compliance_note`) produces a **hold**
when the migration would take it away from instances that have not performed it yet. A hold
is a refusal the operator may accept **by name**:

```
POST /api/v1/definitions/versions/migrate
{ "node_mapping": {...}, "acknowledge": ["opsApprove"] }
```

**Every request is a dry run unless it says `"dry_run": false`.** The reply is the plan
either way; only an explicit `false` applies it. Until 2026-09-25 the flag was read the
other way round from how it is documented here — a request that left it out, like the
examples on this page, moved the instances.

Three properties make this worth having:

- **Only pending instances count.** An instance that already gave the approval waived
  nothing and must not read as though it did.
- **The obligation must *land* on a node that carries one too.** Existing is not enough:
  mapping `opsApprove` onto `salesApprove` lands the token perfectly and still means nobody
  performs the check. A version that keeps the node ID but drops the marking has removed the
  control just as surely as deleting the node would.
- **Node IDs, not a blanket flag.** An override people set once and forget is one they stop
  reading, and it would carry over to whatever the plan holds next time.

The acknowledgement and the authoriser's name are written to every affected instance's
timeline. This is entirely additive: a definition that marks nothing behaves exactly as
before.

### Node actions — deciding work instead of moving it

A node mapping can only ever answer *where does this work go*. B2 and B3 above ask something
else, and the only way to say either with a mapping alone is to point the work at some other
step — which is how somebody else's approval ends up being performed by the wrong person.

```
POST /api/v1/definitions/versions/migrate
{
  "node_actions": {
    "opsApprove": { "kind": "skip", "reason": "the operations manager role was eliminated" }
  }
}
```

- **`skip`** cancels the work parked on the node and advances the instance past it as though
  it had been performed. The engine's own `Proceed` is what runs, so boundary timers are
  cancelled, multi-instance counts are honoured and the following gateway is evaluated
  exactly as it would have been. Reimplementing that here would have been a second set of
  BPMN semantics, and the two would drift. The advance runs on the **source** graph, because
  the node being skipped is precisely the one the new version does not have, so only the old
  graph knows what follows it.
- **`cancel`** ends the instance where it stands: open tasks are cancelled, tokens cleared,
  status set to `cancelled`. The instance is **not** migrated — it will never run again, and
  its record should name the version it actually ran. Pending timers are left alone, because
  `timerStillApplies` already refuses to fire one for an instance that is not active, which
  is what a terminate end event relies on too.
- **`hold`** leaves the instance on the source version and raises an incident at the node for
  somebody to decide. An instance already held at that node keeps its one open incident, and
  nothing more is recorded for it.

**A decision is made on the instance as its lock finds it.** Which action to try is chosen
from the listing, and the listing is as old as the run. So each action reads the instance
again once it holds the instance's lock, and acts only on one that is still running and
still has a token on that step. An instance that has moved on in between — its holder
completed the step, or it finished — is left exactly as it is: nothing is withdrawn,
advanced, cancelled or raised, no ledger row and no trail entry are written, and it is not
moved to the new version in that run either, because the plan was made for where it used to
be. It stays on the version it is running. The server log names it (*A migration passed over
an instance that was no longer where its listing found it*) with the run's id; the reply does
not — an apply that passed one over still answers `applied: true` — so a dry run of the same
migration afterwards is how to see what is left, and running it again plans for where the
instance now stands. A skip used to advance such an instance a second time and record the
approval its holder gave as waived; a cancel ended it; a hold raised an incident at a step
it had left.

`cancelled` is a new instance status. Reusing `completed` would have made an instance that
was called off read, in every list and every count, exactly like one that succeeded; `failed`
is no better, because nothing went wrong — somebody decided.

Refused, because doing any of these half-way is worse than not doing them:

| Refusal | Why |
| :-- | :-- |
| A skip, cancel or hold with no reason | Without one the trail cannot tell a step nobody performed from a step somebody did |
| A skip, cancel or hold with a reason of more than 2,000 characters | The instance's ledger row cannot hold it. Refused in the plan, so a dry run and an apply agree: left to the ledger it would stop an apply at the first instance on that node, after the ones ahead of it had been moved |
| A node that is both mapped and actioned | Two contradictory instructions; guessing is how the wrong one gets applied |
| Skipping a node with no outgoing flow | Nowhere to advance to |
| Skipping a gateway | Several outgoing flows: which branch would it have taken? |
| A skip when no engine is wired | Refused rather than half-performed |

Work on an actioned node is exempt from the "must land somewhere" check — refusing a
migration for stranding the very task the caller asked it to cancel would make the feature
unreachable.

Each decision writes its own trail entry — `node_skipped`, `instance_cancelled` or
`instance_held` — naming the node, the authoriser and the reason, and a row in the instance's
ledger (see *Audit* below). Separate from the migration entry because it is a separate fact,
and the one an auditor actually asks about: not *this instance changed version* but *this
approval did not happen, and here is who said so and why*.

The entry and the row are written in the change's own transaction, so a decision that cannot
be recorded is not made. For a skip that is the withdrawal and the advance; for a cancel, the
cancellation; for a hold, the incident, which now runs in a transaction that locks the
instance, as a cancel's does. The migration stops at the instance it could not record, names
it, and says how many had been dealt with; running it again carries on. A decision's entry
used to be written after the fact, or logged and carried on when it failed. A cancel also no
longer writes `instance_cancelled` for an instance it found, once locked, no longer running.

### Re-derived assignment

A task that changes node is **rebuilt from the node it lands on** — name, description, type,
priority, due date, form, assignee, candidate users and groups — and its claim is dropped.
A task that lands on a step naming nobody is then an administrator's or an operator's to
take, like any task with no assignee and no candidates.

Carrying the task across was an authorisation bug: a task mapped from `opsApprove` onto
`salesApprove` kept the operations manager as assignee and candidate group, which let the
person whose step had just been deleted complete the step that replaced it, while the sales
manager never saw it. Camunda preserves the assignee across migration, but only because it
requires the two activities to be semantically equivalent first; nothing here can establish
that, so the safe default is the other one.

### Audit

Every migrated instance gets an `instance_migrated` entry naming the source and target
version, which work was re-pointed, who authorised it, and which controls were waived.
Written outside the unit of work: a migration that succeeded should not roll back because
the audit write failed, and a lost entry is logged loudly. The decisions above are not
written that way: their entries are part of the change.

**The ledger.** What was done to an instance outside its process is also kept as rows in
`instance_deviations`, which can be asked for by instance, where the trail is read as a
timeline. A migration writes one row per decision on each instance, and the rows of one
migration share its `run_id`:

| Act | Row | Reaches | What it records |
| :-- | :-- | :-- | :-- |
| `skip` | `waive` | the task | the tasks withdrawn, as they were and as they are (status and assignee), and how many |
| `cancel` | `cancel` | the instance | its status, `active` to `cancelled`, and the tasks withdrawn |
| `hold` | `hold` | the instance | the incident raised |
| an acknowledged control the instance had not yet performed | `control_waived` | the instance | the step, and its `compliance_note` when it has one |

A `control_waived` row has no reason, because whoever acknowledged the loss signed for every
instance at once; it is written once per lost step, in the same transaction as the rewrite and
before any of it, and an instance that had already performed the step gets none. Each row
names the trail entry that tells the same act, and the entries of a skip, cancel or hold name
their row in `deviation_id`. The `instance_migrated` entry, written after the rewrite commits,
is the one a control-loss row names; if that entry is lost, which is logged, the row names an
entry that does not exist. A migration that only moves work and waives no control writes no
row.

**A `hold` row says the hold was placed, not that it is still open.** A hold is an incident
raised at the step, and resolving that incident (`POST /api/v1/incidents/{id}/resolve`, an
operator's call) writes no row and changes none: nothing rewrites a ledger row once it is
written, so `after.incident.status` reads `open` for ever. Resolving it leaves no trail entry
and names nobody either; the incident's own `status` and `resolved_at` are all that say it
happened. To see whether a hold is still open, read the instance's incidents
(`GET /api/v1/incidents/{instanceId}`) and find the one whose `id` is the row's
`after.incident.id`. A hold does not stop the step's holder from completing it, and a later
run of the same migration, finding the incident resolved and the instance still on the step,
places a new hold and writes a new row. Whether releasing a hold becomes a recorded act is
decided with the hold of one instance in place, which is not in this release.

**What writes no row, by decision.** The ledger is for what somebody did to an instance that
its process did not decide. These change an instance too, and are left out on purpose:

| Lever | Who | Why no row | Where it shows |
| :-- | :-- | :-- | :-- |
| Resolving an incident, a hold's included | operator | It retries the failed work behind an incident, when there is any; it decides nothing about the process | The incident's `status` and `resolved_at` only: no trail entry, no name |
| Sending a message, broadcasting a signal | any member (message), operator (signal) | It is the event the process was modelled to wait for | The trail shows the instance moving on from the waiting step, and not who sent it; governing that channel is its own piece of work |
| An operator or administrator claiming a task nobody was named for | operator, administrator | The modelled fallback for such a task | The trail's claim entry names who took it |
| The engine withdrawing tasks: approvals a met completion condition no longer needs, a task a boundary event interrupts, work parked for outside workers when an ad-hoc sub-process finishes | the engine | The process decided it | A withdrawal entry on the trail (`parked_work_withdrawn` for parked work) |
| A holder handing on or editing their own task | the holder | No reason is required of them | The hand-over's trail entry |
| A migration that only moves work and waives no control | administrator | It decides no step's work | `instance_migrated` |

Hand-overs and edits of a task by somebody who does not hold it, and a step started inside an
ad-hoc sub-process, write rows as well; the changelog lists every writer. Anyone signed in to
the instance's organization reads them with `GET /api/v1/instances/{id}/deviations`
([Watching instances](integration.md#watching-instances)). Only `before` and `after` are
stored encrypted, as variables are. `reason`, `actor`, `node_name` and `details` are stored
in plain text: the reason is what a person typed, and the trail's sentence already keeps it
in plain text; nothing written to `details` is a business value. The reply carries no
account ids, and the server acting with nobody signed in is written as `System`, a name an
account may also have, so each row says which it was: `actor_is_server` is `true` exactly
when the row names no account.

### Scheduled cutovers

`DeleteDefinition` now refuses a version a future release names. A version scheduled for
next Monday has run nothing, so every other check passed it — and deleting it left the
timeline naming a version that was not there, whereupon the reader falls back to the highest
one and whatever draft was deployed in the meantime goes live at the scheduled moment.
Unattended, with no error raised anywhere.

---

## 6. Resuming a partial run

A migration writes instance by instance, each in its own transaction, so a failure part-way
leaves some moved and some not. There is deliberately **no run table** to resume from,
because the source version already is one: an instance that moved is no longer on it.
Re-running the same migration therefore picks up exactly what is left.

Three things make that true rather than merely plausible:

- **Anything not still running is skipped.** A migrated instance has left the source
  version; a cancelled one must not be cancelled twice; and a *finished* one must never be
  repointed at a graph it did not execute — that record is the only account of what it
  actually did.
- **`hold` is idempotent.** A held instance stays on the source version by design, so every
  later run finds it again. It does not collect an incident per run.
- **An instance a decision passed over is still on the source version.** One that left the
  step between the listing and its lock is neither decided nor moved, and is not counted
  among those dealt with, so the next run finds it and plans for where it now stands.
- **Every entry of one run shares a `run_id`**, so the trail reads back as "what did that
  migration do" rather than as unrelated events sharing a timestamp.

A failure reports how many instances had already been dealt with, and says to run the same
migration again to carry on.

## 7. The two that were open

Both are now closed, and neither the way it was first written down.

**Delegation-of-authority thresholds** were already implemented. "Under 50k the team lead,
over 50k the CFO" is a value-banded matrix, and it is business policy — it changes far more
often than the shape of the process does, so it is not in the diagram. A user task names a
decision table through `assignment_decision_key` and takes its assignee from whatever that
table decides: the process says *that* somebody approves, the table says who. The mechanism
had unit tests and nothing that ran an instance through it; it does now
(`authority_test.go`).

The band is read once, when the task is created, and that is the last moment before the
work exists — there is no API to amend an instance variable behind a task that is already
open, so the approver a matrix chose cannot go stale under it. If one is ever added, that
test needs a second half.

`separation_of_duties` and the authority matrix answer different questions and compose:
one is *not the same person as the step before*, the other is *who, given the number*.

**The completion/migration race now has a reproducing test**, and writing it found the bug
described in Class D. The seam is in the test, not in production code: a repository wrapper
hooks `ListByDefinition` and runs the completion at the moment the migration's listing
returns — which is precisely the window. No goroutines, no timing
(`stalewrite_test.go`). The probabilistic test stays alongside it: it covers interleavings
the deterministic one cannot name, and it is the one that caught this in CI.

---

## 8. For the quotation process specifically

The operations-approval node almost certainly writes variables the sales manager's step
reads, and `opsApprove → salesApprove` is a rename. Before this work, that one
plausible-looking mapping would have stranded the bookkeeping, carried the wrong authority,
and potentially produced an incident storm — three independent failures from one line of
JSON.

It is now refused or corrected on every count — and the mapping is no longer the only thing
you can say. If those pending operations approvals are moot, the honest instruction is

```json
{ "opsApprove": { "kind": "skip", "reason": "the operations manager role was eliminated" } }
```

which advances each quotation to the sales manager, leaves the operations manager's task
cancelled rather than transplanted, and writes on every one of those thirty timelines that
the approval did not happen, who decided that, and why.

**Drain is still the right answer for a healthy population**; what changed is that the day
you hit a §3 emergency and do not have that luxury, the migration will not quietly make
things worse.

## See also

- [`recovery.md`](recovery.md) — the migration runner only goes forward
- [`../AGENTS.md`](../AGENTS.md) §0 — why a silent default at a decision point is an incident
- `server/domains/services/impl/migration.go` — the planner and the apply
- `tests/instancemigration/state_test.go` — every case in §2 that has a test
