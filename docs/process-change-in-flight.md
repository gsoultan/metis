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

The other direction of the same window was the worse one. An instance that *arrived*,
between the listing and its lock, on a step the plan did not find it on was rewritten as it
then stood. When that step was one the new version does not have — the step a skip was meant
to clear, reached a moment too late to be skipped, or any removed step in a migration that
only moves work — it arrived on the new version with a token and an open task on a step that
version lacks. The planner's "must land somewhere" check had run on the listing, and the
rewrite never asked again of the row its lock returned. The apply answered `applied: true`.
The task's holder could still complete it; the token came off, nothing followed, and the
instance stayed `active` with no token and no task, which nothing in the product can then
move on, end or hold. Both are closed now, and both demonstrated
(`lands_or_passed_over_test.go`, with the completion placed in the window on purpose):

- **The work is decided for whoever holds it.** Before it decides an instance's work the
  apply reads the instance again, so one that has reached a step being skipped, cancelled at
  or held at since the listing gets that decision — the same row, the same entry — as one
  that was there all along.
- **An instance is moved only if its work lands, asked under its lock.** In the rewrite's own
  transaction, on the row its lock returned and before anything is written, the planner's
  landing check runs again for that one instance. See *An instance is moved only if its work
  lands*, below.

An instance that has not moved since it was listed passes the check it passed in the plan and
is migrated exactly as before (`unmoved_pins_test.go`).

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
| Work parked where the target has no node | The original check; a stranded token cannot be un-stranded. Asked in the plan of every instance, and again of each instance under its lock before it is rewritten |
| Engine bookkeeping with nowhere to land | Names the counter, not just the token riding on it |
| Two counters merging onto one node | No correct way to add two arrival counts together |
| A boundary event moved off its activity | A timer firing against work that is not running |
| A boundary event mapped onto a node that is not a boundary event | Its timer or waiting message would act on that node: a deadline mapped onto the approval it watched completed the approval when it came due. A boundary event is mapped only to a boundary event |
| A mapping naming a node the target lacks | A typo, wrong regardless of what is running |
| Different process keys or projects | Not a version change |
| **An unacknowledged control obligation** | See below |

### Warnings (do not block)

- A downstream gateway with a **default flow**, which would route silently instead of raising
  an incident when a variable the removed step used to set is missing.
- Tasks **claimed or delegated right now**, which go back to the queue.
- A **redirect of a step instances have completed, or one that carries a control**: open work
  moves to the step mapped to, and work already done on the old step does not count as done
  on the new one, so a control or a separation-of-duties rule there will not see it. See *A
  rename and a redirect*, below.

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
  nothing and must not read as though it did. "Already gave" is read from the instance's own
  record of completed steps, and that record follows a mapping only where it renames a step.
  It used to follow every mapping: a finished step redirected onto a control the instance was
  only waiting at made the control read as passed, and a later migration that dropped it held
  nothing and asked nobody (`control_after_redirect_test.go`).
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

**A decision is made on the instance as its lock finds it.** The listing is as old as the
run, so before it decides an instance's work the apply reads the instance again, and tries an
action at every decided step the instance holds a token on in either reading: in the fresh
one, so that an instance which reached the step after the listing is decided like the rest;
in the listing's, so that one which has left the step is found to have left it. Neither
reading decides anything. Each action reads the instance once more when it holds the
instance's lock, and acts only on one that is still running and still has a token on that
step. An instance that has moved on in between — its holder completed the step, or it
finished — is left exactly as it is: nothing is withdrawn, advanced, cancelled or raised, no
ledger row and no trail entry are written, and it is not moved to the new version in that run
either, because the plan was made for where it used to be. It stays on the version it is
running, and running the same migration again plans for where it now stands. A skip used to
advance such an instance a second time and record the approval its holder gave as waived; a
cancel ended it; a hold raised an incident at a step it had left.

**An instance is moved only if its work lands, asked under its lock.** The plan answers "does
everything this instance holds have somewhere to go on the new version" from its listing. The
rewrite asks it again, of the row its lock returned, in its own transaction and before it
writes anything, with the planner's own check and the same mapping and decisions the plan was
given. An instance is left alone — not re-pointed, nothing written, still on the version it is
running — when, by then,

- it holds a token, an open task, a timer or a queued service call that can still run
  (pending, running or failed), a waiting event or
  a join or multi-instance counter on a step the new version has no step for and the mapping
  does not cover, or counters on two steps the mapping puts onto one: what the planner
  refuses a migration for. A timer or a call that has already run is not work and is not
  counted, in the plan or here: it cannot be deleted, so it stays behind as a row, and it used
  to refuse the migration of an instance that had long since passed a wait the new version
  dropped;
- or it still has a token on a step the migration skips, cancels at or holds at. In the plan
  such work needs nowhere to land, because it is to be decided. By the time of the rewrite the
  decisions have been made, each in its own transaction before it, so a token still there is
  one no decision settled: the instance reached the step in the few statements between the
  apply reading it again and locking it, or a skip left part of a repeating step behind;
- or it has an open task or a waiting event on such a step, one the new version lacks, with no
  token under it. A decision acts on the instances waiting at its step, so nothing reaches
  that work. The engine does not leave any behind (leaving a step lets go of what waits on it
  and withdraws its tasks), so this is a rule for a state only a defect would produce. A
  timer is deliberately not part of it: a skipped wait leaves its timer behind, pending,
  because a job cannot be deleted, and the engine dismisses it when it comes due and finds no
  token for it. The instance is moved with it and does not wait for that hour
  (`TestPinASkipOfATimerWaitMovesTheInstanceAndItsTimerIsDismissedWhenDue`). The same
  exclusion covers a queued service call left on a skipped step, and there it is not harmless:
  the call is still made when its job runs, and re-pointed at a version without the step the
  job fails for want of its node and ends as an incident (read from the job worker, not run;
  the same before this work; in the roadmap).

Two more questions are asked before those, and neither is about where the work lands:

- **Was the plan made for this instance?** An apply plans first and then lists the instances
  again to work through them. Everything a plan establishes about an instance — that its work
  lands, which controls it loses, and that somebody accepted the loss — is established for
  the instances of the first listing. One that arrived on the source version between the two
  — started there, as a request does while the new version is staged and not yet live, or
  moved there by another migration — is left alone. It used to be moved, and where every
  planned instance had already passed a control step the new version drops, it lost that
  control with no acknowledgement asked and no `control_waived` row.
- **Is it still on the version being migrated from?** Asked of the locked row, first. A second
  run of the same migration, started while the first is still working (a client retrying a
  slow apply), lists the instance on the old version and reaches it after the first has moved
  it. It used to apply the mapping again to wherever the instance then stood — with a mapping
  that chains, on to the next step, the one in between passed without anybody performing it —
  and a decision naming a step the instance now stood on, on the new version, was taken on
  it: skipped along the old version's graph, cancelled, or held. Each decision asks the same
  under its own lock.

This also holds for an instance that never moved by itself. A skip advances an instance onto
the step after the one skipped, and the plan was made for where it stood before: when that
next step is one the new version lacks too, with no mapping and no decision of its own, the
skip stands and is recorded and the instance stays on the version it is running. It used to
be moved there. Likewise when the next step is another one the migration decides, a hold
after a skipped step for example, whether or not the new version has it: the skip stands, and
the instance is left for the next run of the same migration to decide at that step, where it
used to be moved with the decision not made.

The order of the locks is what it was: each decision in its own transaction, instance then
tasks, and then the rewrite in another, instance first. The check reads the instance's tasks,
jobs and waiting events under the rewrite's lock and takes no lock of its own.

**The reply says which instances were left alone.** The plan in the reply was made before
the apply and says what would happen; `passed_over` says what did not:

```
{
  "plan": { ... },
  "applied": true,
  "passed_over": [
    { "instance_id": "0199…",
      "reason": "It was no longer waiting at \"Operations approve\" when the migration reached it, so nothing was decided there and it was not moved. It stays on version 1; if it is still running, run the same migration again to plan for where it now stands." }
  ]
}
```

`passed_over` is always present: `[]` when the run left nobody behind, and for a dry run,
which writes nothing. An instance that finished before the run reached it — which a
migration already left unmoved, whether it decides work or only moves it — is listed
there too, as *no longer running when the migration reached it*. So is one whose work would
not land, in a migration that decides work or one that only moves it, with one of these
reasons, each naming the step as the version it runs names it:

- *When the migration came to move it, it had work at "Operations approve", and version 2 has
  nowhere to put that, so it was not moved. It stays on version 1. Plan the migration again
  for where it now stands: it needs a mapping, or a decision, for that work.* A dry run of the
  same migration is now refused for the work parked on that step, which it names by its id.
- *When the migration came to move it, it was waiting at "Operations approve", where this
  migration decides the work rather than moving it, and no decision had settled it, so it was
  not moved. It stays on version 1; run the same migration again to decide it where it now
  stands.* Running the same migration again finds it at the step and decides it.
- *…it was part-way through two steps that this mapping moves onto one, and their progress
  cannot be added together…*, for counters the mapping would merge.
- *When the migration came to move it, it had a task or a waiting event at "Operations
  approve", where this migration decides the work of the instances waiting there, and it was
  not waiting there, so no decision reached that work and version 2 has nowhere to put it. It
  was not moved and stays on version 1. Nothing in the product withdraws a single task or
  waiting event yet, so it stays there until that work is gone.*

Two reasons are not about where the instance stood:

- *It was not on version 1 when this migration was planned: it started, or was moved there,
  after that. Nothing had been asked about it, so nothing was decided about it and it was not
  moved. It stays on version 1; plan the migration again to include it.* A dry run now counts
  it, and holds on any control it has not passed.
- *It was no longer on version 1 when the migration reached it: another run of a migration had
  already moved it. Nothing was decided about it and it was not moved again.* There is nothing
  to do: the run that moved it wrote its `instance_migrated` entry.

`applied` keeps its meaning, whether anything was written: `true` when the run acted on at
least one instance, whatever it passed over, and `false` when it passed instances over and
acted on none. An instance a skip advanced and the rewrite then left alone counts as acted
on, and is listed as well. The server log also names each instance that had left its step
(*A migration passed over an instance that was no longer where its listing found it*),
each whose work would not land (*A migration passed over an instance that holds work the new
version cannot take as it stands*), each the plan was not
made for (*…that was not on the source version when it was planned*) and each already moved
(*…that another run had already moved off the source version*), with the run's id.

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
| A skip, cancel or hold of a node no instance ever waits at | The decision would be made about nobody. The refusal names the node, says what kind it is, and what to decide instead. See below |

Work on an actioned node is exempt from the "must land somewhere" check — refusing a
migration for stranding the very task the caller asked it to cancel would make the feature
unreachable. That is the plan's rule. When the instance is rewritten, a token still on an
actioned node is no longer exempt, and neither is an open task or a waiting event left there
with no token: see *An instance is moved only if its work lands*, above.

**A decision is taken where an instance waits.** A skip, a cancel or a hold acts on the
instances holding a token on the node it names. A decision naming a node the engine never
leaves a token on used to pass the plan, be taken on nobody and write nothing, and the apply
reported an instance acted on: it had been moved, and where the node was a boundary event,
with the event's waiting message still on a node its new version does not have, because the
exemption above excused it. The plan now refuses such a decision, and a dry run shows it. The
rule is read off the engine, one node type at a time — when a token arrives, does the node's
handler return with the token still there?

| Node | An instance waits there | Why |
| :-- | :-- | :-- |
| User task, manual task | yes | A task is created and the token stays |
| Service task | yes | A job or an external task is queued |
| Catch event, timer event | yes | A subscription, a timer or a condition |
| Call activity | yes | The token stays while the process it called runs |
| Ad-hoc sub-process | yes | The token stays until its completion condition is met |
| Parallel or inclusive gateway that joins | yes | A branch that arrives early keeps its token there |
| Escalation throw | not refused | Its handler does not advance it; left decidable |
| Boundary event | no | Executed with no token put on it; the token is on the step it is attached to |
| Sub-process, embedded or event | no | The token is taken off and put on the steps inside |
| Start event, end events | no | Passed through, or the token is removed |
| Exclusive and event-based gateways, and a gateway that only splits | no | The token is taken off and put on what follows |
| Script task, business rule task | no | Run and advanced at once |
| Events the process throws (message, signal, compensation, intermediate) | no | Thrown and advanced at once |
| Pool, lane | no | Not steps |

The refusal names the node as people know it and says what to decide instead:

> *hold of "Checks" cannot be taken: it is a sub-process, and an instance inside one waits at
> the steps inside it, never at the sub-process itself, so the decision would be made about
> nobody; decide the steps inside it instead: "Check the request"*

> *hold of "The customer withdrew" cannot be taken: it is a boundary event, and no instance
> ever waits at a boundary event, only at the step it is attached to, so on its own the
> decision would be made about nobody; name the event together with "Approve the request",
> deciding both, and what waits on the event ends with that step, or map the event to a
> boundary event the new version has*

One case is accepted as it always was: a boundary event named **together with the step it is
attached to**. An approval with a deadline, both dropped by the new version, is the commonest
shape a removed step has; a migration that decides the approval is refused for the
deadline's timer, which has nowhere to land, unless the deadline is named in a decision too.
Nothing is taken or recorded for the event there either. What waits on it ends with its step:
a skip lets go of the event's waiting message (read from the engine's advance) and leaves its
timer to be dismissed when due, a cancel ends the instance, a hold leaves it on the version
it runs (`TestPinADecisionNamingAStepAndTheDeadlineOnIt`, which has a deadline's timer). The
kind and the reason given for the event are not acted on and appear only in the plan's
`actions`. The refusal for the event's work says so itself: after *map each one to a node it
does have* it adds, for a boundary event, that it can be mapped only to a boundary event, and
otherwise to decide its step and name the event in the same decision.

**When the new version keeps a step and drops a boundary event on it.** The event cannot be
mapped onto the step (below) and cannot be decided on its own. An instance waiting at the
step is decided there with the event named beside it: a `hold` keeps it on the version it
runs, with the event still armed, and raises the incident. Once it has left the step, the
same migration run again finds nothing of it at the step and moves it; what is left of the
event by then is at most its timer, which the plan lets stand because the event is named
with its step, and which the engine dismisses when it comes due
(`TestAnInstanceAtAStepWhoseDeadlineWasDroppedIsHeldAndMovedOnceItHasLeftTheStep`).

Two things have no path yet, and are in the roadmap: a decision on the start of an event
sub-process, which is refused like any start event; and a boundary event on a sub-process,
which cannot be named with its step because a decision on the sub-process is itself refused.

**A boundary event is mapped only to a boundary event.** What sits on a boundary event is a
timer or a waiting message that acts on the node it sits on. The planner used to look at a
mapped boundary event only when its target was one too, so a deadline mapped onto the
approval it watched — the new version keeps the approval and drops the deadline — passed
the plan, and when the three days were up the approval was recorded as performed and the
instance finished with the approver's task still open. That mapping is now refused, in the
dry run and so in the apply (`TestABoundaryEventMayNotBeMappedOntoAStep`). Still accepted,
read and not run: a step mapped onto a boundary event, and the start of an event sub-process
mapped onto a step; both are in the roadmap.

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

An **open** task that changes node is **rebuilt from the node it lands on** — name,
description, type, priority, due date, form, assignee, candidate users and groups — and its
claim is dropped. A task that lands on a step naming nobody is then an administrator's or an
operator's to take, like any task with no assignee and no candidates.

**A mapping moves only what is still open.** A mapping says where work in progress goes.
Until this was fixed (0.4.0 has it) the rewrite applied it to every task and every job of the
instance whatever its status, and since a task that changes node is rebuilt and offered, a
task completed weeks before came back `claimed` or `unclaimed` on the step the mapping
named: a quotation whose operations approval had been given, waiting for the sales manager
and migrated with `opsApprove → salesApprove`, had two open sales approvals for one token,
and no task said any longer that the operations approval had been given or by whom.
[A task a migration reopened](upgrading.md#a-task-a-migration-reopened) finds them. What
each kind of row does now:

| Row | Follows the mapping | Left exactly as it is |
| :-- | :-- | :-- |
| The instance's tokens, and its join and multi-instance counters | All of them, under any mapping: live work, read against the graph the instance now runs | — |
| The instance's lists of completed and compensated steps | Under a **rename** only | Under a redirect: the step stays in the record under its own id |
| Tasks `unclaimed`, `claimed`, `delegated` — the statuses the landing check counts and the engine withdraws | Rebuilt from the step they land on, under any mapping | — |
| Tasks in every other status, which is `completed` and `canceled` (the engine sets no other) | Under a **rename** only, and then the step's id alone: one column, written by a statement guarded by the status | Everything else always, and under a redirect the whole row |
| Jobs (timers, queued service calls) | `pending`, `running`, and `failed` — a failed job runs again when its incident is resolved — pointed at the new version and the mapped step | `completed`: still names the version and the step it ran on |
| Waiting events | Every one; a row exists only while the instance waits | — |
| Incidents | None: a migration has never rewritten an incident, open or resolved | All |
| Work parked for an outside worker (`external_tasks`) | None, and the landing check does not read it either (read, not run; in the roadmap) | All |

#### A rename and a redirect

A mapping has two shapes, and they mean different things for work already done.

- A **rename** says *this step is that step*: the id it maps to is not a step of the version
  being migrated from, and no other step is mapped onto it (`submit → request`, where
  `request` is new). Whoever did the submit did the request.
- A **redirect** says *send the open work over there*: the id it maps to is a step of the old
  version too (`opsApprove → salesApprove`), or several steps are sent to one. It says
  nothing of the kind about an operations approval already given.

Work in progress follows either. Finished work follows only a rename, and then only by its
step's id: a completed or cancelled task keeps its status, its assignee, its name, its form
and its timestamps, and the instance's record of completed steps names the step by its new
id. Under a redirect neither is written: the record must not say somebody did a step they
did not do.

This matters to two things that read the record against the version the instance now runs.
A `separation_of_duties` rule finds who performed a step by reading the instance's completed
tasks by step id. After a migration that only renamed the steps, the finished task had kept
the old id, the rule named the new one, and the person who submitted a request could claim
and complete its approval (`TestAfterARenameWhoeverDidOneHalfOfAFourEyesCheckMayNotDoTheOther`;
0.3.0 moved the id and held the rule, 0.4.0 reopened the task). And a compliance hold asks
the completed-steps list who has passed a control (above).

After a redirect, then, a rule or a control on the step mapped *to* does not see work done on
the step mapped *from*, and should not: it was a different step. The plan says so in a
warning whenever an instance it covers has completed the redirected step, or the step
carries a control, so that it is somebody's decision and not a surprise.

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

These make that true rather than merely plausible:

- **Anything not still running is skipped.** A migrated instance has left the source
  version; a cancelled one must not be cancelled twice; and a *finished* one must never be
  repointed at a graph it did not execute — that record is the only account of what it
  actually did.
- **An instance another run already moved is not moved again.** The rewrite asks, under the
  instance's lock, whether it is still on the source version, so two runs of the same
  migration at once move each instance once. The one that finds it moved names it in
  `passed_over`.
- **An instance the plan was not made for is left for the next plan.** One that arrived on
  the source version after the apply planned is not moved and is named in `passed_over`;
  planning again includes it.
- **`hold` is idempotent.** A held instance stays on the source version by design, so every
  later run finds it again. It does not collect an incident per run.
- **An instance a decision passed over is still on the source version.** One that left the
  step between the listing and its lock is neither decided nor moved, and is not counted
  among those dealt with, so the next run finds it and plans for where it now stands. The
  reply to the apply names it in `passed_over`.
- **So is an instance whose work would not land.** One that reached, after the listing, a
  step the new version cannot take is not moved and is not counted among those dealt with.
  The reply names it in `passed_over` with what to do: run the same migration again when the
  step is one the migration decides, and plan again — a dry run now refuses, naming the
  step by its id — when it needs a mapping or a decision the migration did not have.
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
