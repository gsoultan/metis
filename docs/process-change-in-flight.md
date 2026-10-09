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
instance stayed `active` with no token and no task, which nothing in the product could then
move on, end or hold (it can be closed now: *In-place waive, cancel and hold*, below). Both
are closed now, and both demonstrated
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
- A **redirect of a step running instances have completed, or one that carries a control**: open work
  moves to the step mapped to, and work already done on the old step does not count as done
  on the new one, so a control or a separation-of-duties rule there will not see it. See *A
  rename and a redirect*, below. A step given a new id that is marked as a control and does
  not stand where the old step stood is warned of the same way.

A warning does not block an apply, and it does not ask anybody either. Some of what a plan
does is not one administrator's to apply at all: see
[A second administrator](#a-second-administrator).

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
  record of completed steps, and that record follows a mapping only where it renames a step
  — and onto a step marked as a control, only where the step is renamed where it stands
  (*A rename and a redirect*, below).
  It used to follow every mapping: a finished step redirected onto a control the instance was
  only waiting at made the control read as passed, and a later migration that dropped it held
  nothing and asked nobody (`control_after_redirect_test.go`).
- **The obligation survives only if the step itself survives, still marked.** That is the
  same id, or the step renamed where it stands, landing on a node that is marked too.
  Existing is not enough: a version that keeps the node ID but drops the marking has removed
  the control just as surely as deleting the node would. Landing on a marked step is not
  enough either: mapping `firstCheck` onto `secondCheck` lands the token on a control and
  still means nobody performs the first. That used to count as carried across, so a mapping
  could take an instance past a control with nothing held and nobody asked
  (`TestAControlRedirectedOntoAnotherControlIsAControlNotCarriedAcross`). And a new id is not
  enough to make it the same step: `firstCheck` mapped onto a third check the new version
  adds after the second is a hold too
  (`TestAControlMappedOntoAControlNewInTheTargetIsAControlNotCarriedAcross`).
- **Node IDs, not a blanket flag.** An override people set once and forget is one they stop
  reading, and it would carry over to whatever the plan holds next time.

A hold is counted over every instance the plan lists that has not passed the step, ended
ones included, so a plan can hold for a control only finished instances missed.

**An acknowledgement is not the end of it.** Acknowledging a hold makes the plan
applicable. Where at least one instance that has not ended has not passed the step, the
plan then says `requires_second_approver: true`, and an apply is not made: it is sent to a
second administrator. See [A second administrator](#a-second-administrator).

The acknowledgement and the authoriser's name are written to every affected instance's
timeline. A definition that marks nothing has no hold. It is not untouched by the second
administrator, though: a skip asks whatever is marked, and so does a version that takes a
separation-of-duties rule away.

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
  graph knows what follows it. A user task or a manual task is ended whole instead, by the
  engine's `FinishActivity`: every token on the step goes, every open task is withdrawn, and
  the instance moves on once. For a step that runs once that is the same thing. For an
  approval several people give it is not: `Proceed` counted one run, left the other runs'
  tokens on the step with their tasks withdrawn, and started the next run of an approval that
  asks one person after another, so the instance was passed over and took one run of the
  migration per remaining run to leave the step
  (`TestASkippedRepeatingApprovalLeavesNoRunBehind`). Every other kind of step that repeats
  is still advanced past with `Proceed`.
- **`cancel`** ends the instance where it stands: open tasks are cancelled, work parked for
  outside workers is withdrawn, waiting events are dropped, the instance's open incidents are
  closed, tokens cleared, status set to `cancelled`. The instance is **not** migrated — it
  will never run again, and its record should name the version it actually ran. Pending
  timers are left alone, because `timerStillApplies` already refuses to fire one for an
  instance that is not active, which is what a terminate end event relies on too. A queued
  service call is left as well, and is settled without calling when its turn comes: a call
  asks whether its instance has ended before it is made. One already on its way when the
  instance is cancelled cannot be recalled; its result is not written. Parked work and open
  incidents used to be left behind: a worker was still handed the work, and its report moved
  the cancelled instance on (`TestACancelWithdrawsTheWorkParkedForWorkers`,
  `TestACancelClosesTheIncidentsOpenOnTheInstance`).
- **`hold`** leaves the instance on the source version and raises an incident at the node for
  somebody to decide. An instance already held at that node keeps its one open incident, and
  nothing more is recorded for it.

**A skip is not one administrator's call.** A `cancel` and a `hold` are applied by the
request that asks for them. A plan with a `skip` in it, over a version at least one
unfinished instance runs on, says `requires_second_approver: true`, and its apply is
answered 202: the migration is recorded as a request and nothing is moved until a different
administrator approves it. See [A second administrator](#a-second-administrator).

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
  apply reading it again and locking it, or a skip left part of a repeating step behind
  (a step other than a user or manual task, which a skip ends whole);
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
      "cause": "left_the_step",
      "steps": [ { "node_id": "opsApprove", "name": "Operations approve" } ],
      "steps_in_all": 1,
      "reason": "It was no longer waiting at \"Operations approve\" when the migration reached it, so nothing was decided there and it was not moved. It stays on version 1; if it is still running, run the same migration again to plan for where it now stands." }
  ],
  "passed_over_in_all": 1
}
```

`passed_over` is always present: `[]` when the run left nobody behind, and for a dry run,
which writes nothing. It lists the first 200 instances the run passed over, in the order
the run came to them, and `passed_over_in_all` is how many there were. `cause` says why as
one of eight codes, for a client that words it in its own language or acts on it; `steps`
is the steps the cause is about, by id and by name, at most ten of `steps_in_all`; `reason`
is the same thing as a sentence, and still names every step. The codes are set out in
[Integrating with Metis](integration.md#the-instances-a-run-passed-over). The reply to an
approval of a migration lists them the same way. An instance that finished before the run reached it — which a
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

### In-place waive, cancel and hold

A node action needs a second version of the process to migrate to. One instance can be
decided where it stands, on the version it runs, with no second version:

```
POST /api/v1/instances/{id}/deviations
{
  "kind": "waive",
  "node_id": "opsApprove",
  "reason": "the operations manager is on leave and the order ships today",
  "outputs": { "approved": true }
}
```

`kind` is `waive`, `cancel` or `hold`. What each does to the instance is what a migration's
`skip`, `cancel` and `hold` do: one implementation, called by both. What differs is who
decides and how. It is an administrator of the instance's organization, about one instance,
after a preview of that instance. A cancel and a hold are made by that administrator's
apply. **A waive is not: its apply asks for it, and it is made when a different
administrator approves** ([A second administrator](#a-second-administrator)). The request
and the reply are set out in
[Integrating with Metis](integration.md#waiving-cancelling-or-holding-one-instance), and
what an operator does in [the runbooks](runbooks.md#waiving-cancelling-or-holding-one-instance).

**Every request is a dry run unless it says `"dry_run": false`.** A dry run reads the
instance, holds no row and writes nothing. Its reply is the plan: the open tasks the act
would take, every reason it cannot be made, what to know first, and a `visit_key`. Every
refusal that is true is listed, not the first one, so one preview shows everything there is
to fix. An apply is the same request with the plan's `visit_key` and `"dry_run": false`.

**An apply is decided on the instance as its lock finds it.** It is one transaction. The
instance's row is locked first, and everything below is asked of that row:

1. Was this request already made? Then it is answered with the record the first one wrote
   and `replayed: true`, whatever the instance has become since, and nothing is done again.
   That is what makes a lost answer safe to ask for again. For a waive that still waits,
   the record is the waiting row and the answer is the 202 again, to whoever asked and to
   nobody else.
2. Is the instance still running? One that has ended is refused: *this instance is
   cancelled, so it can no longer be waived; preview again*.
3. Is the plan, made again from the locked row, the plan that was previewed? The
   `visit_key` is compared, and when it differs the apply is refused: *this instance has
   moved since you previewed it; preview again*. Then the plan's own refusals, which are
   what a preview of the same request shows.
4. Does the instance still wait where the act is made?
5. For a cancel or a hold: the act, its ledger row and its trail entry, together. If any
   of them fails, none is kept. For a waive: the request, a ledger row that waits
   (`pending_approval`) and a `deviation_requested` trail entry, together, and nothing
   else. No task is withdrawn, nothing is set, nobody is told, and the instance does not
   move.

An approval of the waive asks 2 to 4 again, of the row its own lock returns, and then makes
the waive: an approved waive is applied to the instance as it stands when it is approved,
not as it stood when it was asked for.

A completion of the step takes the instance's lock too, so a completion and a waive of one
step take turns, and the second finds what the first left
(`TestACompletionThatArrivesWhileItsStepIsBeingWaivedIsRefused`,
`TestAWaiveAppliedAfterItsHolderFinishedTheStepIsRefused`). Two applies of one preview sent
at the same moment act once (`TestTwoAppliesOfOnePreviewWaiveOnce`,
`TestAppliesOfOnePreviewSentTogetherActOnce`). So do an approval and a completion
(`TestAnApprovalAndACompletionTakeTurnsAtTheInstance`), and two approvals of one request
(`TestTwoApprovalsAtOnceWaiveOnce`).

**The visit key says what work the plan was made for.** It is made from the instance, the
version it runs, the kind of act, the step, and:

| Kind | Also in the key | So the key changes when |
| :-- | :-- | :-- |
| `waive` | the tokens on the step and the open tasks on the step | a task of the step is completed, opened or withdrawn, or a token arrives there or leaves |
| `hold` | those, and each incident on the step with whether it is open or resolved | the same, or an incident on the step is raised or resolved |
| `cancel` | every token and every open task of the instance | anything open on the instance changes, on any branch |

Who holds a task, the instance's variables and its status are not in the key, so a claim or
a hand-over between the preview and the apply does not make the preview stale. The record is
then written from the tasks as the apply found them, not as the preview listed them
(`TestAWaiveRecordsWhoHeldTheWorkWhenItWasTaken`,
`TestACancelRecordsWhoHeldTheWorkWhenItWasTaken`). The key covers every open task and
token, whether the plan lists it or not.

A second request naming a key that has had its act, and asking for something else (another
reason, other values), is refused with who acted: *this step was already waived by boss*.
While a waive of the visit waits, anything else asked of the visit, and the same waive
asked by anybody but its requester, is refused with the request that waits: *A request to
waive “Operations approve” is already waiting for approval (request 0199…, asked by boss);
approve or reject that one.* A preview says so too, as a refusal, before an apply would.

#### Waive

A waive ends a step somebody was to do without anybody doing it: its open tasks are
withdrawn, each holder is told theirs was withdrawn, and the instance moves on from the step
once. None of that happens when it is asked for. It happens when a second administrator
approves the request, and until then the step's work can still be done by whoever holds
it. It is recorded as **waived**, never as performed. The tasks end `canceled`, not
`completed`. No completion is announced. The trail's entry is the `node_skipped` a
migration's skip writes, with `outcome: "waived"`, and reads

> *“Operations approve” was waived — nobody performed it — by boss. Reason: the operations
> manager is on leave and the order ships today.*

One thing does not tell the two apart: the instance's own list of completed steps includes a
waived step, as it does after a migration's skip. To tell waived from performed, read the
task (`canceled`), the trail (`node_skipped`, `outcome: waived`) or the ledger, not the
instance.

**A step marked as a control is waived like any other, and says so.** It waits for a
second administrator as every waive does, and the warning below is in the plan the request
keeps for them to read
(`TestAWaiveOfAControlWaitsWithItsWarningAndIsApprovedWithItsMark`). A waive asks for no
acknowledgement of a step marked `compliance_relevant`. Its plan warns *“Operations approve”
is marked as a control. Waiving it is recorded as a control that was not performed.*, and
the waive's ledger row and trail entry carry `control: true` (in the row's `details`, and
in the entry's data); a waive of any other step carries neither
(`TestAWaiveOfAControlSaysSo`). The waive's own row is the record that the control was not
performed. The instance's list of completed steps counts the waived step all the same, so a
later migration that drops the step treats this instance as having passed the control and
writes no `control_waived` row for it (read from the code, not run).

The plan refuses a waive that would have to guess:

| Refused | In the plan's words |
| :-- | :-- |
| A step that is not a person's work | *“Screen the supplier” is work for a system, not a person; retry it or resolve its incident instead of waiving it.* For a call activity whose called process is running: *“Have it checked” runs another process; waive the step inside that process (instance 0199…) instead.* For a call activity the instance has not reached: *“Have it checked” runs another process; waive the step inside that process instead.* For one whose called process has ended without resuming it (cancelled in place, or ended at a terminate end event): *“Have it checked” is waiting for a process that has ended and will not resume it; this instance can be cancelled or held instead.* For anything else: *…is not work somebody does; hold the instance instead.* |
| A step the instance is not waiting at | *This instance is not waiting at “Sales approve”.* |
| A step nobody has a task for | *Nobody has “Operations approve” to do, so there is nothing to waive.* |
| A step with several ways out | *“Pick a supplier” has 2 ways out, so waiving it would choose a branch on the business's behalf.* |
| A step with no way out, unless it is inside an ad-hoc sub-process, whose completion condition is read again instead | *…has no way out, so there is nowhere for the instance to go once it is waived.* |
| A step that runs once and that the instance reached several times at once | *“Check the order” was reached 2 times at once on this instance, and a waive would move the instance on only once. Complete or reassign its tasks instead, or hold the instance.* |
| A step the instance's version does not have | *This process has no step "archive".* |
| No reason | *Say why: a reason is required, and it is kept with the record.* |
| A reason of more than 2,000 characters | *The reason is longer than 2000 characters; say it more briefly.* |
| An instance that is not running | *This instance is completed; only a running instance can be waived.* |

An approval several people give is ended whole, on purpose: every open run is withdrawn and
the instance moves on once, in parallel or one after another
(`TestWaivingAParallelApprovalWithdrawsEveryOpenRunAndAdvancesOnce`,
`TestWaivingASequentialApprovalStartsNoFurtherRun`). A waive inside an embedded sub-process
moves on inside it, and one in a called process that ends it resumes its caller.

**What the waiver counts as.** A step that is waived sets nothing unless the waive says
what. `outputs` is values for fields the step's form declares, set before the instance moves
on, as though somebody had filled them in. A waive is not a variable editor:

- Only a field the step's form declares can be set, and only one the form of every open
  task of the step declares. `METIS_ALLOW_UNDECLARED_TASK_VARIABLES`, which lets a completion
  set anything for a migration window, does not apply to a waive
  (`TestAWaiveSetsOnlyWhatTheStepsFormDeclares`).
- At most 50 values, at most 64 KiB as JSON, a name of at most 255 characters, and no value
  given as `null`: a null is not a value, and whatever reads the field would decide on
  nothing.

**Every place that decides from the step must be given its value.** This is the rule of
`AGENTS.md` §0, no silent default at a decision point, applied to one instance. The whole
definition is read, not only what follows the step: an event sub-process, a boundary event
on an enclosing sub-process, a step started by hand in an ad-hoc sub-process and a branch
running beside the step are all reached without a flow from it. Each place that reads a field the step's form declares is
listed in `decision_points`, and the waive is refused until `outputs` gives every such
field a value:

| `kind` | What reads the field |
| :-- | :-- |
| `gateway` | The conditions on the flows of an exclusive or inclusive gateway |
| `conditional_event` | A condition a step waits for |
| `completion_condition` | The completion condition of a repeating step or an ad-hoc sub-process |
| `decision_table` | The decision table a business rule task consults, or the one that assigns a user task, at the version the step pins, with the decisions it requires |
| `collection` | The list a repeating step repeats over |
| `called_process` | A call activity that hands the field to another process. Never read: see below |

> *“Approved?” decides from approved, which “Review the claim” would have set; say what the
> waiver counts as by supplying approved.*

Three rules that are easy to assume the other way:

- **A value the instance already holds does not count.** On a second visit through a loop
  the instance holds the first visit's answer. Routing on it is the silent default, so the
  waive is refused until it says what this visit counts as
  (`TestAValueLeftFromAnEarlierVisitDoesNotCountForTheGateway`).
- **A default flow excuses nothing.** A gateway with a default flow is listed with
  `has_default_flow: true` and refused for a missing value like any other.
- **Being asked for more than the instance will meet is the accepted cost.** A place the
  instance has already passed, or will never reach, is listed too. The value asked for is
  one the step's own form declares, so it can be given — with three exceptions, each of
  which the plan refuses for: a field only some of the step's open runs declare, a field
  whose name is longer than 255 characters, and more than 50 values in all.

`missing` on the plan is the complete list of what has still to be supplied whenever the
waive can be made: it holds at most 50 names, each to 255 characters, and a waive that would
need more, or a longer name, is refused for that. A place names only the first few fields it
is missing.

**What is not read.** Each is a warning, not a refusal, and the waive can be applied past
it. Check before applying.

- **A process a later step calls.** A call activity hands the called process the step's
  fields, and what that process decides from them is in another definition: *“Check the
  supplier” starts another process and hands it approved, which “Review the claim” would
  have set; that process was not read, so check what it does with it before applying.*
- **The process that started this one.** When a called instance ends, its values go back to
  its caller, which decides from them: *This instance was started by “Supplier onboarding”
  at “Run the checks”, which receives its results when it ends and was not read. Where that
  process decides on a value this step would have set and you give none, it decides on the
  value it already holds, or undoes the waive if it holds none. Check that process before
  applying.* Both halves are demonstrated: a caller holding a value for the field decides on
  that older value and the waive is applied
  (`TestAWaiveInACalledProcessLetsItsCallerDecideOnAValueItAlreadyHolds`); one holding none
  has a gateway with no way out, and the waive is undone in both instances
  (`TestAWaiveAGatewayCannotFollowIsA400ThatSaysWhoseGatewayItWas`).
- **A condition that could not be read**, such as one written as a `js:` script: *“Route by
  script” could not be read to see what it decides from; check it before applying.* And a
  decision past the 64 tables one preview reads.
- **A value derived from the step's.** A script or a service task's input mapping that
  computes another variable from one of the step's fields is not traced. A gateway reading
  that other variable is not listed.

**When a value fits no branch.** A waive whose value no flow of a gateway accepts, where
the gateway has no default, is found out when the waive is made, which is at the approval.
The approver is refused with a 400 that names the gateway, and nothing is changed: *The
values given fit no way out of “Large order?”, so the waive was not applied and nothing was
changed. Preview again and give a value one of its branches accepts. The request is still
waiting: reject it, and the waive can be asked for again.* The approver cannot change the
values, and a corrected request is refused while this one waits, which is why the sentence
says what to do. The gateway may be one of another instance the advance reached: the
caller's, or a process a later step calls. The sentence then says whose
(`TestAWaiveAGatewayCannotFollowIsA400ThatSaysWhoseGatewayItWas`,
`TestAWaiveWhoseApprovalCouldNotAdvanceIsWithdrawnAndAskedAgain`).

#### Cancel

A cancel ends the **whole instance**, whichever step it names. The step is where the record
says the instance stood. Every open task is withdrawn and its holder told, work parked for
outside workers is withdrawn, waiting events are dropped, the instance's open incidents are
closed, its tokens are cleared and its status is `cancelled`. The plan lists every open
task of the instance, on every branch, and warns of each that is with somebody, of parked
work to be withdrawn and of incidents to be closed.

A cancel that names **no step** closes an instance that is `active` and waits at no step:
the state a migration of an earlier release could leave, and one a process reaches by itself
when its last step has no flow leaving it. It is the only supported way to close one. Its
plan warns *This instance is not waiting at any step. Cancelling it closes it.* Only where
the instance waits was looked at: whether a timer or a message could still move it was not.
The same cancel withdraws a task left open with no token under it, and warns of it: *“Record
the outcome” is still open though the instance is not waiting there; it will be withdrawn.*

| Refused | In the plan's words |
| :-- | :-- |
| An instance while a process it called has not ended | *This instance is waiting on 1 process(es) it started (0199…); cancel or finish those first.* |
| A cancel naming no step, of an instance that waits somewhere | *This instance is waiting at “Operations approve”; say which of those steps it is to be ended at.* |
| A step the instance is not waiting at | *This instance is not waiting at “Sales approve”.* |
| A step the instance's version does not have, and the instance holds no token on | *This process has no step "archive".* |

A cancel may name a step the instance holds a token on **that its version does not have**:
the state a migration of an earlier release could leave
([upgrading.md](upgrading.md#an-instance-a-migration-left-with-nothing-to-do), case 1).
The step is shown by its id, in the plan and in the record, because the version has no name
for it; a waive and a hold of such a step are refused with *This process has no step "…".*
(`TestACancelCanNameAStepTheInstancesVersionNoLongerHas`, on a row written through the
repository).

A **called instance** can be cancelled in place, where it waits or when it waits nowhere.
Cancelling it ends that instance and resumes nobody. While its caller has not ended, its
plan warns *This instance was started by another process (instance 0199…), which is still
waiting for it and is not resumed by this; cancel or hold that one next.* The caller is
left where it was, at its call step, and is refused while a process it called has not
ended. So to end a process and what it called, cancel the called instance first, then the
caller at its call step (`TestACalledInstanceIsCancelledAloneAndItsCallerOnlyAfterIt`; for
a called instance that waits nowhere, with a cancel that names no step,
`TestAStrandedCalledInstanceAndItsCallerCanBothBeClosed`). When the caller has already
ended — a terminate end event on another branch ended it while the process it called was
still running — there is no such warning, and the cancel closes what the caller left behind
(`TestACalledInstanceWhoseCallerHasEndedIsCancelledWithNoWarningOfIt`).

What a cancel leaves, in place as in a migration:

- **A call already on its way cannot be recalled.** A call to another system that is in
  flight when the instance is cancelled is still made. Its result is not written, and a
  failure raises no incident. A call still queued is not made: it is settled without calling
  when its turn comes, and until then the instance's job list shows a pending call on a
  cancelled instance.
- **Timers lapse quietly.** A pending timer is not deleted. It comes due, finds the
  instance is not waiting for it, and does nothing: no token, no task, no incident
  (`TestATimerDueAfterACancelInPlaceMovesNothingAndRaisesNoIncident`).
- **No event says the instance was cancelled.** Each withdrawn task raises `TaskCanceled`,
  on the event stream and to webhooks. A cancel of an instance with no open task raises no
  event at all.

#### Hold

A hold raises one incident at the step the instance waits at, for somebody to decide, and
changes nothing else: the tokens, the tasks and the status are as they were, and **the
step's work can still be done**. A hold makes an instance visible. It does not stop it.

- The incident's text is *held at “Operations approve” by boss:* and the reason. Anyone who
  may read the instance's incidents reads it, which is anyone signed in to its organization.
  Put nothing in the reason that only an administrator should see. (The incident of a
  migration's hold begins otherwise: *held out of the migration from version 1 to version 2
  of "quotation-approval" by boss:* and the reason.)
- A step that already has an open incident keeps that one, and no second is raised. The
  plan warns *“Operations approve” already has an open incident; the hold will use it.* The
  hold is recorded all the same, and its entry says so: *This instance was held at
  “Operations approve” by boss; the incident already open on that step stands.* The
  incident's text is not rewritten, so this hold's reason is in its ledger row and its trail
  entry only.
- On a step that calls a system, or parks work for a worker, the incident already open may
  be the engine's own, raised when the work failed. A hold uses it like any other.
- A hold needs a step. An instance that waits nowhere can be cancelled and cannot be held.

**Letting a hold go** is resolving its incident (`POST /api/v1/incidents/{id}/resolve`, an
operator's call). Nothing records that: no trail entry, no ledger row, no name. The
incident's `status` and `resolved_at` are all that say it happened. After that the step can
be held again. A new preview has a new `visit_key`, because the key covers the step's
incidents, and applying it raises a new incident and writes a new row
(`TestAHoldCanBeMadeAgainOnceItsIncidentIsResolved`).

Three things about a hold that are gaps, not design:

- **The inbox words a hold as a failure.** It shows every incident under its step's name and
  the word *failed*, with a *Try again* button, and explains the incident's text as a
  technical cause. A hold reads the same way. *Try again* on a hold's own incident resolves
  it. On an engine's failure incident that a hold used, *Try again* retries the failed call
  as it always did. And on a step that parks work for a worker, resolving any incident
  there offers that work again if it had run out of retries (read from the code, not run).
- **Resolving takes no lock on the instance and asks nothing of it.** An incident can be
  resolved a moment after a hold found it open. The hold's row is true of the moment it was
  written.
- **A hold's incident outlives its instance.** If the step is completed and the instance
  goes on to finish, the incident stays open until somebody resolves it.

#### Limits that hold for all three

- **The lists in a plan are the first of what there is.** A process is somebody's input, so
  a plan has a size whatever the process: at most 100 decision points (those missing a value
  first, then those not read), 200 open tasks, 200 called instances, 50 missing names, and
  ten names at a point, each shown to 64 characters. The counts beside them
  (`open_work_in_all`, `decision_points_in_all`, `called_instances_in_all`,
  `missing_in_all`, and a point's `reads_in_all` and `missing_in_all`) are of everything. They are exact, with one exception: where several
  steps of a definition share an id, a point's own two counts may count a name twice, never
  too few. The refusals are worked out from everything, not from what is listed, and
  `applicable` is the one answer to "can this be applied". An empty list does not mean
  nothing.
- **An apply waits.** It waits for the instance's lock, and a cancel, or the approval of a
  waive, then waits for the row of each task it withdraws, behind a claim, a hand-over or
  an edit under way. There is no deadline on the server: the wait ends when the lock is
  free or the client goes away.
- **A suspended instance is refused.** A preview lists *This instance is suspended; only a
  running instance can be held.* An apply answers *this instance is suspended, and a
  suspended instance is not waived, cancelled or held in place*, for all three kinds.
  Nothing in the product suspends an instance or resumes one today, so this is met only on
  a row changed outside it. An approval of a waive of a suspended instance is refused in
  the apply's sentence and the request goes on waiting
  (`TestAWaiveRequestForASuspendedInstanceWaits`).
- **A cancel and a hold are one administrator's decision, and stay so.** Each is applied by
  the request that asks for it and answers 200
  (`TestACancelAndAHoldStillApplyOnOneAdministratorsCall`). Only a waive waits for a second
  administrator.

### A second administrator

A waive, and a migration that loosens a rule on work still running, are not applied by the
administrator who asks for them. The apply records a **request**, answers 202 and changes
nothing. A different administrator of the same organization approves it, and the approval
is what makes the change. What an approver does is in
[the runbooks](runbooks.md#approving-a-request-for-a-second-administrator); the routes are
in [Integrating with Metis](integration.md#requests-for-a-second-administrator); what an
upgrade changes is in
[Upgrading](upgrading.md#a-second-administrator-approves-waivers-and-skips-migration-34).

#### What asks

**A waive in place, always.** Every plan of a waive says `requires_second_approver: true`,
and the service sends a waive to a request whatever its plan says
(`TestAWaiveNeedsASecondAdministratorWhateverItsPlanSays`). A cancel and a hold in place
never ask.

**A migration, for four things.** Each is counted over the instances the plan lists that
have **not ended**: active ones, suspended ones, and any in a state the server does not
know. A plan over a version nothing unfinished runs on asks nobody, whatever it does
(`TestASkipOverAVersionWithNothingRunningAsksNobody`). An instance has **passed** a step
when the step's id is in the instance's list of completed steps, and nothing else is
asked.

| The plan | Asks when | In the plan's words |
| :-- | :-- | :-- |
| **Skips a step** (`node_actions`, `kind: skip`) | always, whether or not anything waits at the step now | *“Operations approve” would be skipped for every listed instance waiting at it when the migration runs — not only those waiting there when this was asked for — and nobody would perform it* |
| **Does not carry a control across**: a step marked `compliance_relevant` that the new version drops or unmarks, or that the mapping sends to a different step (a new id that is not a rename in place counts), its loss acknowledged | at least one unfinished instance has not passed it | *“Operations approve” carries a control 3 instance(s) have not passed, and they never would* |
| **Redirects a step**: a mapping entry whose key is a step of the version being migrated from, that maps it to another id, and that is not a rename in place (below). An entry from a step to itself, or from an id that version does not have, is none | some unfinished instance has not passed some step, marked as a control, of the version being migrated from. One sentence for each such entry | *“Prepare” would be redirected to “Sign” for every listed instance waiting at it when the migration runs — 2 wait(s) there now — and a control such an instance has not passed may no longer be ahead of it: “Legal review”* |
| **Loosens separation of duties**: a step's `separation_of_duties` rule names a step that the rule of a step an unpassed instance can come to perform in its place no longer names — the step the mapping lands on, and the step the new version keeps under the same id when the mapping sends it elsewhere — or that every such rule still names though the new version no longer has it | some unfinished instance has not passed the step that carries the rule | *“Approve” would no longer be refused to whoever performed “Submit”: the new version does not keep that separation of duties, and 2 instance(s) have not passed “Approve”* |

A `cancel`, a `hold`, and a mapping that does none of these apply on one call, as before
(`TestCancelHoldAndMappingOnlyMigrationsStillApplyOnOneCall`).

The redirect's sentence has two more wordings, for a step nobody waits at now and for one
where everybody waiting has passed every control; each says a control *may* no longer be
ahead, never that it is lost. The separation-of-duties sentence has a second wording where
the new version kept the rule under the id a mapping sends the named step to, and the work
already done on that step does not follow the mapping — *…the new version's rule names the
step that is mapped onto, but work already done does not follow that mapping, so the rule
would not find who did it…* — and a third for a
rule left naming a step the new version dropped: *…whoever performed it before the
migration is still refused, but nobody can perform it afterwards, so the rule refuses
nobody new…*. A rule that is unchanged and names a step neither version has asks nobody
(`TestAnUnchangedRuleNamingAStepNeitherVersionHasAsksNobody`).

`second_approver_reasons` lists the first ten sentences, sorted, and one more that counts
the rest: *and 4 more reason(s) of the same kinds, not listed here*. Whether somebody is
asked is decided from all of them. A step is named by its name, or by its id when it has
none, cut at 255 characters
(`TestTheReasonsAPlanListsAreBoundedWhereverTheyAreSaid`).

**A rename in place asks nobody; any other mapping of a step is a redirect.** A mapping
entry `from → to` is a rename in place when all of these hold:

- `to` is not a step of the old version, and no other step is mapped onto it;
- `from` is not a step of the new version: the old id is gone;
- the same steps lead to it and follow it. The steps the old version's flows into `from`
  come from, read through the mapping, are the steps the new version's flows into `to`
  come from; and the same for the flows out.

Only the step's own flows are compared. What lies beyond its neighbours is not looked at.
A boundary event, an event sub-process, a step with no sequence flow at all, and a mapping
onto an id the new version lacks are never in place
(`TestAShapeWithNoPlaceOfItsOwnIsNeverRenamedInPlace`). So, in a sentence an administrator
can use: *to rename a step without anybody being asked, give it a new id and change nothing
else about where it stands; rename it and move it, and it is a redirect.*

**What this asks for that you might not expect.** The rule over-asks on purpose, because
the other direction is an instance past a control with nobody asked.

- **Any redirect in a process with a control somebody has not passed asks**, even when the
  control is still ahead of where the instance lands. Nothing walks the graph to find out.
- **A renamed step whose neighbours also changed asks**, in such a process: a step added
  or removed next to it, a flow added to or taken from it, a neighbour that was itself
  redirected. If the renamed step is itself a control, it is a hold to acknowledge.
- **A skip asks even when nobody waits at the step.** What is approved is the decision.
- **A version whose only instance is suspended asks**
  (`TestASkipOverAVersionWhoseOnlyInstanceIsSuspendedStillAsks`).
- **A hold can need an acknowledgement and no second administrator**: when only ended
  instances had not passed the control.

#### What it does not review

**The second administrator is asked about skips, controls not carried across, redirects
past controls and separation-of-duties rules taken away. Nobody reviews the whole
difference between two versions.** A new version can be laxer in ways the planner does not
detect, and a migration onto it then applies on one administrator's call. Each of these is
read from the planner's code, not run:

- **Wider candidate groups, a lower completion condition** on an approval several people
  give ("2 of 3" to "1 of 3"), **a changed gateway condition.** None is read.
- **The same step ids rearranged, or a control bypassed.** A version that keeps every id
  and every marking and moves a control to behind where an instance stands, or routes the
  flow round it, needs no mapping, so nothing is a redirect and nothing is dropped.
- **A redirect of a step a `separation_of_duties` rule names.** The rule still names the
  step, and the new version still has it, so nothing is counted as lost — but the work
  that waited there is now done at the step the mapping sends it to, by somebody the rule
  does not look for. In a process that marks a control some instance has not passed, the
  redirect itself asks; in one that marks nothing, nothing does.
- **A step renamed in place in front of a control that moved.** In place compares one
  step's own flows.
- **A step that carries a rule, removed outright, when it is not marked as a control.** An
  approval protected only by `separation_of_duties`, in a process that marks nothing, can
  be redirected past or dropped.
- **Controls that exist only in the new version.** Nothing asks whether an instance lands
  beyond one.
- **"Passed" means "in the instance's list of completed steps".** A step that was waived,
  a step a migration skipped, and a step completed once before a loop brought the instance
  back to it all count as passed. So a control waived in place does not make a later
  migration that drops it ask for that instance.

Mark the steps that are controls. An unmarked approval is protected only against its rule
being taken away, and only where a `separation_of_duties` rule names it and the step that
carries the rule survives. A redirect past it, or a redirect of the step its rule names, is
not detected in a process that marks nothing.

What *is* compared, for a step that carries a rule: the rule of every step of the new
version that an instance which has not passed the step can come to perform in its place.
That is the step the mapping lands on, and — when the mapping sends the step elsewhere
while the new version keeps a step under the same id — that step too, because an instance
that has not reached the step yet is not there to be moved and comes to the one under the
old id. A name counts as kept only when each of those rules keeps it, so a mapping entry
is no way round a rule the step under the old id has lost
(`TestAMappingDoesNotSilenceALoosenedSeparationOfDuties`).

The other direction over-asks, and is left that way: a control in a branch an instance
never takes stays "not passed" for that instance for ever, so every redirect in that
process keeps asking for as long as such an instance runs.

#### What it does not protect against

**The second approver protects against a mistake, and against a decision nobody else
looked at. It does not protect against an administrator who creates, removes or displaces
accounts.** The requester and the approver are told apart by account id
(`TestTheRequesterNeverApprovesTheirOwnWaiveEvenRenamed`). The check is that two accounts
were used. It cannot tell that two people used them.

- **An administrator can create a second administrator account and approve as it.** The
  approval is recorded as an ordinary second approval, and the account can then be
  deleted. No setting is needed for this (read from the code, not run).
- **In an organization named as having one administrator**, an administrator can take
  another administrator's role away, approve their own request, and give the role back
  (run once over the routes when the exception was built, and not kept as a test).
- **What is left is evidence, in one place.** Creating an account, changing its roles,
  changing its roles in one organization and deleting it each write one line to the
  server's log with who did it and to whom — a change that changes nothing writes none —
  (`TestEveryChangeToWhoHoldsWhichRoleIsLoggedWithWhoMadeIt`). Nothing else records them:
  there is no trail of changes to accounts, and a membership carries no dates. Keep the
  server's log somewhere an administrator of Metis cannot edit it.
- **Also logged, in a line of the same shape that names no actor:** the first administrator
  that set-up creates (`made_through: set-up`), and a password set with `--reset-password`
  (`made_through: --reset-password, run on the server`).
- **Not logged at all:** a sign-in through the identity provider that changes which
  organizations an account belongs to, and a rename of an account that changes no role.
- **One administrator the others cannot displace.** On an installation with more than one
  organization, the Administrator role held on the account, which acts in every
  organization the account belongs to, is granted and taken away, and such an account
  deleted, only by a platform administrator: one the operator names in
  `METIS_PLATFORM_ADMINS`. An organization's own administrators cannot remove it. On an
  installation with one organization any holder of that role can strip or delete another.
- **Two smaller windows.** An account the identity provider signs in is counted as an
  administrator of the organizations its latest sign-in placed it in
  (`TestAnAccountAnIdentityProviderPlacesIsCountedWhereItsLatestSignInPutIt`). And the
  role check on a route reads a caller that may have been cached for up to
  `METIS_AUTH_CACHE_TTL` (five seconds unless set), while the count of other
  administrators reads the database: for that long the two can disagree.
- **A request does not outlive its requester's standing — at the approval.** An approval
  asks whether the account that made the request is still there, still belongs to the
  organization and still holds the administrator role in it, and closes the request as
  `stale` when it does not
  (`TestAWaiveIsNotApprovedOnceItsRequesterNoLongerAdministers`,
  `TestAMigrationIsNotApprovedOnceItsRequesterNoLongerAdministers`). It is asked once,
  when the approval is made, of the accounts as committed then: a role taken away a moment
  later is not seen, and a role taken away and given back between the asking and the
  approval leaves no mark on the request. A rejection does not ask.
- **Whoever holds the database or the server's command line is not bound by any of it.**

**And it covers what it covers.** One administrator can still, alone: cancel or hold an
instance; hand an open task to anybody in the organization the step's separation of duties
does not bar, themselves included, with a reason, after which that person can complete it —
recorded as performed by them, with a ledger row for the hand-over, not as waived (read
from the hand-over's code, not run here); and migrate onto a version that is laxer in one
of the ways above. "A waive needs a second administrator" does not mean one administrator
can never act alone.

**Nor does it keep a waiting waive from being read.** A waive that waits can be read
before it is decided. Anyone signed in to the instance's organization reads its row with
`GET /api/v1/instances/{id}/deviations`: the reason, the values it asks for
(`after.variables`), who asked and the request's id. Only an administrator can read or
decide the request itself. The same read goes on returning the rows of requests that were
never carried out — rejected, expired, stale — each saying so in its `status`.

#### The request

A request keeps what was asked (the command, as it will be carried out), what the
requester was shown (the plan, with why it needs somebody else in `because`), who asked,
and a deadline fixed when it was made: 72 hours on unless `METIS_DEVIATION_APPROVAL_TTL`
says otherwise (`TestTheApprovalWindowIsFixedWhenTheRequestIsMade`). The command and the
plan are stored encrypted, as process variables are. The reason, who asked and who
decided, the list of instances and the outcome are stored in plain text.

**One request for one thing.** A request for a waive is for one visit of one step. A
request for a migration is for one policy: both versions, the mapping, each decision with
its reason, the controls acknowledged, the controls the plan found dropped, and the
instances it was narrowed to. Who asks is not part of it. So the same thing asked again by
whoever asked is answered with the request that waits, and asked by anybody else it is
refused with that request's id: a second administrator does not get a request of their own
to approve by asking for the same thing
(`TestAWaiveAskedForAgainIsAnsweredWithTheRequestThatWaits`,
`TestTwoAsksForOneMigrationAtOnceMakeOneRequest`).

**Who may decide.** An administrator of the request's organization, with an account.

- *Approve:* anybody but the requester. A note is optional. The requester is refused, 403:
  *You asked for this. A different administrator has to approve it.* The attempt changes
  nothing and is written to the server's log, where alone it leaves a mark.
- *Reject:* any administrator, with a reason. The requester among them: that is a
  withdrawal, and the record says which it was. There is no route of its own for it.

**A request's life.**

| Status | Means | Reached from |
| :-- | :-- | :-- |
| `pending_approval` | It waits. Nothing has changed. | made |
| `approved` | A migration only: approved, and its run is going. Never where a request rests. | `pending_approval` |
| `applied` | What it asked for was carried out. For a migration: its run ended without failing, which can be with nothing changed. | `pending_approval` (a waive), `approved` (a migration), `interrupted` (a migration whose run outlived its window, was marked, and then reported) |
| `interrupted` | A migration only: approved, and the run stopped part-way, was refused before it began, or never reported. What it had done stands. | `approved` |
| `rejected` | An administrator said no, or the requester withdrew it. | `pending_approval` |
| `expired` | Nobody decided it before its deadline. | `pending_approval` |
| `stale` | Somebody came to approve it and it no longer held, or whoever asked for it no longer administers the organization. | `pending_approval` |

Nothing leaves `applied`, `rejected`, `expired` or `stale`, and nothing becomes live
again. What is over stays over, and the same thing is asked for afresh
(`TestARequestsStatusMovesOnlyTheWayTheClosedSetAllows`). A waive is never `approved` or
`interrupted`: it is approved and made in one change.

**The clock decides; a pass writes it down.** A request past its deadline reads `expired`
to every reader and cannot be approved from that moment, whether or not anything has
recorded it (`TestAnOverdueRequestReadsAsExpiredBeforeTheSweep`). Every ten minutes, and
once at start-up, each replica records what the clock has decided, on each database. A
pass does two things, in this order: approved runs that never reported are recorded
`interrupted`, then requests past their deadline are recorded `expired`
(`TestTheSweepClosesUnreportedRunsBeforeOverdueRequests`). Each request is closed in a
transaction of its own, so one that cannot be closed is logged and passed, and the rest
are still reached (`TestTheSweepReachesWhatIsBehindRequestsItCannotClose`). A request left
only because another transaction held a row its closing needs is counted apart (`held`),
and a pass that has waited out thirty of those ends there — two seconds each, so a minute
for each of its two reads — and leaves the rest to the next pass
(`TestAPassCountsHeldRequestsApartAndEndsOnceItHasWaitedForEnough`,
`TestAPassCountsARequestSomebodyHoldsApartFromOneItCannotClose`). So
does whoever next meets the request: an approval or a rejection that finds it overdue
records the expiry and is then refused, and asking again for the same thing closes the
overdue request and makes a fresh one
(`TestAWaiveCanBeAskedAgainOnceItsRequestHasExpired`,
`TestAMigrationCanBeAskedAgainOnceItsRequestHasExpired`).

**`stale` is found only by trying to approve.** Nothing watches a waiting request. One
whose instance has moved on, been cancelled or been migrated goes on reading
`pending_approval` until somebody tries to approve it, or it expires
(`TestAWaiveRequestGoesStaleWhenTheHolderCompletesTheStep`,
`TestAWaiveRequestGoesStaleWhenTheInstanceIsMigratedOrCancelled`). The queue of waiting
requests can therefore hold requests that no longer hold.

#### What an approval does

**Of a waive**, in one transaction. The request's row is locked, then the instance's. The
instance must still be running; the plan, made again from the locked row, must be the one
the request pinned — the same `visit_key` — and refuse nothing; the instance must still
wait at the step. Then the waive is made, as an apply made it before this release, and
recorded: the row that waited becomes the row of the waive, naming who asked (`actor`) and
who approved (`approved_by`).

- If whoever asked no longer administers the organization — the account deleted, out of
  the organization, or without the administrator role in it — the request is recorded
  `stale` and the approver is told *The administrator who asked for this, boss, no longer
  administers this organization, so nothing was applied. It has to be asked for afresh by
  somebody who does.* This is asked after the deadline and before anything else.
- If the instance has ended, or has moved, or the plan now refuses: the request is
  recorded `stale` and the approver is told *This request no longer holds — the instance
  has moved since it was asked for — so nothing was applied. Preview again and ask
  afresh.* The record is kept; the 400 comes after it.
- If the instance is suspended, or a gateway has no way out for the values asked for, or
  the server fails: nothing is changed and the request waits as it did.

The plan the request keeps is what the requester was shown. It is for reading, and nothing
is decided from it (`TestWhatASecondAdministratorReadsIsThePlanTheRequesterWasShown`).

**Of a migration**, in three steps, none inside another's transaction.

1. *The approval.* The request's row is locked. Whoever asked must still administer the
   organization, as for a waive, or the request is recorded `stale`. The stored migration
   is planned again. If
   what it asks for can no longer be planned, if the plan now refuses, if it is no longer
   the policy that was asked for, or if an unfinished instance is on the version that the
   request did not list, the request is recorded `stale` and the approver refused.
   Otherwise it is recorded `approved`, and that is committed. A plan that could not be
   made because a read failed leaves the request waiting
   (`TestAnApprovalThatCannotPlanJustNowLeavesTheRequestWaiting`).
2. *The run.* The migration is applied as any migration is, in the approver's call, under
   the request's id. Its gate plans once more and checks the stored request against that
   plan: approved, within its time, asked for by whoever the run names as authorising it
   (compared by name: the run's options carry no account id — recorded as a limit), the
   same policy, and no unfinished instance the request did not list. What was approved
   is what runs, or nothing does. An instance that arrived between the approval and the
   run stops the run at the gate, before anything is moved
   (`TestAnInstanceThatArrivesAfterTheApprovalStopsTheRunAtTheGate`).
3. *The report.* What the run did is written on the request: `applied`, or `interrupted`.

**What an approval pins, and what it does not.** *An approval is of the decision and of the
instances the request lists. The decision applies to every listed instance waiting at the
step when the migration runs, not only those waiting there when it was asked for.* The
approver read "1 waits there now"; if a second listed instance reaches the step before the
run, it is skipped or redirected under the same approval. An instance the request did not
list is never acted on under it: one that arrives before the approval makes the request
stale (`TestANewcomerMakesAMigrationRequestStaleAndALeaverDoesNot`), and one that arrives
while the run is going is passed over as `not_planned_for`. Fewer instances is still
covered: one that finished or was moved since is simply not acted on.

**An approved run has a window.** It may start until the request's deadline, or one hour
after the approval, whichever comes first. The gate checks that itself, with no pass
having to run (`TestAnApprovedRequestPastItsWindowIsRefusedByTheGateWithNoSweep`). A
request still `approved` when its window closes — the server stopped mid-run, or the run
is still going — reads `interrupted`, and the ten-minute pass records it. A run that was
in fact still going writes its own report over that mark when it ends, with
`reported_after_sweep: true` (`TestARunsOwnReportWinsOverTheSweeps`). While a run is
going, the same migration cannot be asked for again
(`TestAMigrationBeingAppliedIsNotAskedForAgain`); once its window has closed it can, and
asking closes the old request first
(`TestAMigrationLeftApprovedByADeadProcessCanBeAskedAgain`).

**A run that stops part-way.** The approver's call is answered 500, never 200
(`TestARunThatStoppedPartWayIsAnsweredAsAFailureAndTheRequestSaysHowFarItGot`). The request
reads `interrupted`, and its `outcome` says how far the run got: `changed`, `passed_over`
(a count) and `error`, one sentence of the server's own. The failure's own words go to the
server's log and are not stored on the request. After a panic the outcome says
`count_unknown: true` in place of the two counts. What the run had done stands. See
*Resuming a partial run*, below, for what to do next.

#### The record

- **A waive** leaves, on the instance's trail: `deviation_requested` when it is asked for;
  then `deviation_approved` beside the `node_skipped` entry of the waive, or
  `deviation_rejected` (with `rejected_by`, and `withdrawn: true` when the requester ended
  it), `deviation_expired` or `deviation_stale` (with `attempted_by`). Each names the
  request (`request_id`) and who asked (`requested_by`). The `node_skipped` entry keeps
  the sentence it always had, to the letter, and gains `request_id` and `approved_by` in
  its data: the word "approved" is kept out of the sentence of a step nobody performed.
  The ledger row carries `request_id`, `approved_by` and `decided_at`
  (`TestAnApprovedWaiveSaysWhoAskedWhoApprovedAndWhen`).
- **A migration's request has no entry of its own**, because it is for a version and not
  for an instance. Each instance an approved run acts on says so: its `instance_migrated`
  entry, and the entry of each decision made on it, end with *A second administrator,
  deputy, approved this migration (request 0199…).* and carry `request_id` and
  `approved_by`. Its ledger rows name the request, who asked and who approved. **A
  migration request that is rejected, expires or goes stale leaves no trail entry
  anywhere**: the request's own row is the whole record, read with
  `GET /api/v1/deviation-requests/{id}`
  (`TestAMigrationRequestIsRejectedAndExpiredOnItsRowAlone`).
- **An approved redirect leaves no ledger row.** The `instance_migrated` entry is its
  only record on the instance, and so it is written with the move or the move is not
  made: it is the last write of the rewrite's own transaction, and an entry that cannot
  be written undoes that instance's rewrite and stops the run
  (`TestAnApprovedRedirectIsNotMadeWhenItsEntryCannotBeWritten`). The same holds for an
  approved loosening of a separation-of-duties rule, which writes no ledger row either.
  Under an approval the entry lists each step the instance was moved from and to,
  whether or not anybody held a task there
  (`TestAnApprovedRedirectOfAStepNobodyHoldsSaysWhereItMovedFromAndTo`). A redirected
  boundary event's timer or waiting message with no token under it is not listed, and the
  entry does not tell a rename from a redirect (both read from the code, not run).
- **A self-approved migration whose run touched no instance is recorded only on the
  request and in the server's log.** Its request's `outcome` carries `self_approved`,
  `other_administrators` and `organization_id`; no instance has an entry or a row of it,
  because the run wrote to none (read from the code, not run).
- **Trail entries carry names, not account ids.** Who asked and who approved are on the
  request and on the ledger row by account id as well; a trail entry's sentence and its
  data name people by username, as the trail always has.
- **The approver's note is on the request and in the trail's sentence, not on the ledger
  row.** The row says who approved and when.
- **A waive's request and a migration's record the same things the same way.** A request
  its requester approved says so in its `outcome` (`self_approved`,
  `other_administrators`, `organization_id`) for both kinds, and a request found `stale`
  says who found it (`attempted_by`) in its `outcome` for both; a waive says it on the
  instance's `deviation_stale` entry as well
  (`TestAWaivesRequestRecordsASelfApprovalAndAStaleFindingAsAMigrationsDoes`).
- **A rejected waive's ledger row does not say who rejected it.** It says `rejected` and
  when. Who is on the request (`decided_by`) and in the trail entry.
- **A refused attempt leaves a line in the server's log and nothing else**: a requester's
  attempt to approve their own request, and an approval that could not be applied
  (`TestARefusedSelfApprovalAndAnApprovalThatCouldNotAdvanceLeaveATrace`).

#### An organization with one administrator

By default nothing is relaxed: a sole administrator's request waits, and their own
approval is refused (403) in a sentence that begins *You asked for this, and nobody else
administers this organization, so it waits.* They can withdraw it, let it expire, or have
a second **person** made an administrator
(`TestASoleAdministratorsOwnApprovalWaitsUnlessTheirOrganizationIsNamed`,
`TestARequesterAloneInTheOrganizationMayAlwaysWithdraw`).

Whoever operates the installation can name organizations in
`METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS`. In a named organization the requester may
approve their own request while nobody else administers it, and only then:

- "Somebody else administers it" means another account that is not deleted, belongs to
  the organization, and holds the Administrator role there or on the account: exactly the
  accounts whose own approval would be let through
  (`TestWhoCountsAsAnotherAdministratorIsWhoCouldApprove`). It is asked when the approval
  is made, under the request's lock. With one, the requester is refused as anywhere else
  (`TestWithAnotherAdministratorTheRequesterIsRefusedWhateverTheSetting`).
- A reason is required: *Say why you are approving your own request: with nobody else to
  approve it, the reason is the record.*
- The record says nobody else approved
  (`TestASoleAdministratorApprovesTheirOwnRequestAndTheRecordSaysNobodyElseDid`). For a
  waive: a `deviation_self_approved` trail entry that begins *No second administrator
  approved this.*, carrying `self_approved: true`, `other_administrators: 0` and
  `organization_id`; the same three in the ledger row's `details`; and
  `self_approved: true` on the `node_skipped` entry. For a migration: the same three on
  every entry and ledger row of the run and in the request's `outcome`, and each entry
  ends *No second administrator approved this migration: boss approved their own request
  (request 0199…).* The request reads `self_approved: true`. A line is written to the
  server's log.

What the setting is, what the server says at start-up, and when to take an organization
off the list are in
[the runbooks](runbooks.md#an-organization-with-one-administrator). What it does not
close is above: in a named organization an administrator who can change roles can make
themselves the only one.

#### In the migration dialog

The dialog that moves running work to another version reads the plan before anything is
applied. When the plan needs an approval it says so under the heading *This has to be
approved before anything moves*, says the rule — *Nothing moves until it is approved. The
administrator who asked cannot approve it, unless this organization has been set up as
having one administrator.* — lists the plan's reasons, and its button reads *Send for
approval* in place of *Move N instances*. Neither heading says "a second administrator":
that is untrue of an organization set up as having one.

After the press it stays open under *Sent for approval*, with who asked, the same rule,
when the request expires — the date names its time zone — that it is approved or rejected
through the API, there being no screen for it yet, and **the request's reference** (its
id), under *Why an approval is asked* and the plan's reasons. The button beside it reads
*Close*, not *Cancel*: closing does not withdraw the request. It no longer answers a
request that was sent as though nothing had been done.

**A request that was sent is always said.** Closing the dialog over a waiting request — by
*Close*, the "×", Escape or a click outside — raises a toast that says all of the above in
one and **stays until it is dismissed**: once the dialog has gone it is the only place the
reference is. If the form was edited while the apply was on its way, the dialog no longer
shows the request that was sent, so the answer is said in that toast instead. Its title
names the two versions as they were when the button was pressed — *Sent for approval:
v2 → v5* — because the dialog under it may show another plan by then.

An apply that passed instances over stays open too, under *Applied, but not to every
instance* or *This run moved no instance*, and lists them under *Instances that were not
moved*, each with its cause in the reader's language where the catalogue has words for it
and in the server's sentence where it does not.

An answer the dialog cannot read — a 200 or a 202 whose body is not what the route writes
— is said as *The server's answer could not be read*, never as "nothing was moved": the
migration may have been applied, or sent for approval. The message says to check the
instances first, where a request that was sent is found
(`GET /api/v1/deviation-requests`), and that sending the same migration again answers with
that request and makes no second one.

There is no approval screen in this release. The dialog does not list requests, approve,
reject or withdraw one, or show what later became of one. A second administrator decides
through the API, and nobody is notified that a request waits.

### Re-derived assignment

An **open** task that changes node is **rebuilt from the node it lands on** — name,
description, type, priority, due date, form, assignee, candidate users and groups — and its
claim is dropped. A task that lands on a step naming nobody is then an administrator's or an
operator's to take, like any task with no assignee and no candidates.

Carrying the task across was an authorisation bug: a task mapped from `opsApprove` onto
`salesApprove` kept the operations manager as assignee and candidate group, which let the
person whose step had just been deleted complete the step that replaced it, while the sales
manager never saw it. Camunda preserves the assignee across migration, but only because it
requires the two activities to be semantically equivalent first; nothing here can establish
that, so the safe default is the other one.

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
| The instance's lists of completed and compensated steps | Under a **rename** only, and onto a step marked as a control only a rename in place | Under a redirect: the step stays in the record under its own id |
| Tasks `unclaimed`, `claimed`, `delegated` — the statuses the landing check counts and the engine withdraws | Rebuilt from the step they land on, under any mapping | — |
| Tasks in every other status, which is `completed` and `canceled` (the engine sets no other) | Under a **rename** only, and then the step's id alone: one column, written by a statement guarded by the status | Everything else always, and under a redirect the whole row |
| Jobs (timers, queued service calls) | `pending`, `running`, and `failed` — a failed job runs again when its incident is resolved — pointed at the new version and the mapped step | `completed`: still names the version and the step it ran on |
| Waiting events | Every one; a row exists only while the instance waits | — |
| Incidents | None: a migration has never rewritten an incident, open or resolved | All |
| Work parked for an outside worker (`external_tasks`) | None, and the landing check does not read it either (read, not run; in the roadmap) | All |

#### A rename and a redirect

A mapping has two shapes, and they mean different things for work already done.

- A **rename** says *this step is that step*: the id it maps to is not a step of the version
  being migrated from, no other step is mapped onto it, and the id it maps from is no
  longer a step of the new version (`submit → request`, where `request` is new and `submit`
  is gone). Whoever did the submit did the request.
- A **redirect** says *send the open work over there*: the id it maps to is a step of the old
  version too (`opsApprove → salesApprove`), or several steps are sent to one, or the new
  version still has the step under its old id (`prepare → audit`, where the new version
  keeps `prepare` and adds `audit`). It says nothing of the kind about work already done.

Work in progress follows either. Finished work follows only a rename, and then only by its
step's id: a completed or cancelled task keeps its status, its assignee, its name, its form
and its timestamps, and the instance's record of completed steps names the step by its new
id. Under a redirect neither is written: the record must not say somebody did a step they
did not do.

**Onto a control, finished work follows only a rename in place.** A rename looks at ids and
nothing else, on purpose: finished work must go on following a real rename even when the
step's neighbours changed, or a separation-of-duties rule would stop finding who did the
step. One case is taken out. A rename onto a step the new version marks
`compliance_relevant` carries finished work only when the step is renamed where it stands
(*A rename in place*, under [A second administrator](#a-second-administrator)). Otherwise
the completed list and the finished task keep the old id, and the plan warns as for a
redirect. A finished ordinary step must never become evidence that a control was performed
(`TestFinishedWorkDoesNotFollowARenameOntoAControlThatStandsElsewhere`,
`TestAControlRenamedInPlaceCarriesItsFinishedWork`). The cost: a control that really was
renamed while its neighbours changed is not carried for an instance that passed it. That
instance reads as not having passed the new one, and a later migration that drops it holds
for that instance too.

So there are two questions, and they are answered differently. *Does finished work follow
this mapping?* A rename, by ids; onto a control, only in place. *Does this mapping ask for
a second administrator?* Anything that is not a rename in place can, whatever finished
work does.

This matters to two things that read the record against the version the instance now runs.
A `separation_of_duties` rule finds who performed a step by reading the instance's completed
tasks by step id. After a migration that only renamed the steps, the finished task had kept
the old id, the rule named the new one, and the person who submitted a request could claim
and complete its approval (`TestAfterARenameWhoeverDidOneHalfOfAFourEyesCheckMayNotDoTheOther`;
0.3.0 moved the id and held the rule, 0.4.0 reopened the task). And a compliance hold asks
the completed-steps list who has passed a control (above).

After a redirect, then, a rule or a control on the step mapped *to* does not see work done on
the step mapped *from*, and should not: it was a different step. The plan says so in a
warning whenever a running instance it covers has completed the redirected step, or the
step carries a control, so that it is somebody's decision and not a surprise. The number in
the warning is of running instances: the ones the migration would move.

### Audit

Every migrated instance gets an `instance_migrated` entry naming the source and target
version, which work was re-pointed, who authorised it, and which controls were waived. In a
run a second administrator approved it also says who approved, names the request, and
lists each step the instance was moved from and to
(*The record*, under [A second administrator](#a-second-administrator)).
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
| `cancel` | `cancel` | the instance | its status, `active` to `cancelled`, the tasks withdrawn, and in `details`, when there were any, how much parked work it withdrew (`external_tasks_withdrawn`) and how many incidents it closed (`incidents_closed`) |
| `hold` | `hold` | the instance | the incident raised |
| an acknowledged control the instance had not yet performed | `control_waived` | the instance | the step, and its `compliance_note` when it has one |

A `control_waived` row has no reason, because whoever acknowledged the loss signed for every
instance at once; it is written once per lost step, in the same transaction as the rewrite and
before any of it, and an instance that had already performed the step gets none. Each row
names the trail entry that tells the same act, and the entries of a skip, cancel or hold name
their row in `deviation_id`. The `instance_migrated` entry is the one a control-loss row names.
It is written in the rewrite's own transaction, last, for every migrated instance: an instance
is moved with the entry that says so, or it is not moved. A trail that cannot be written undoes
that instance's rewrite and stops the run there, naming the instance, as a ledger row that
cannot be written does (`TestAnInstanceIsNotMovedWhenItsMigrationEntryCannotBeWritten`). It
used to be written after the rewrite had committed, and a lost entry was only logged. A
migration that only moves work and waives no control writes no row.

**A `hold` row says the hold was placed, not that it is still open.** A hold is an incident
raised at the step, and resolving that incident (`POST /api/v1/incidents/{id}/resolve`, an
operator's call) writes no row and changes none: nothing rewrites the row of an act once it
is written, so `after.incident.status` reads `open` for ever. (The one row that is ever
rewritten is a waive's while it waits for a second administrator: once, when its request is
decided.) Resolving it leaves no trail entry
and names nobody either; the incident's own `status` and `resolved_at` are all that say it
happened. To see whether a hold is still open, read the instance's incidents
(`GET /api/v1/incidents/{instanceId}`) and find the one whose `id` is the row's
`after.incident.id`. A hold does not stop the step's holder from completing it, and a later
run of the same migration, finding the incident resolved and the instance still on the step,
places a new hold and writes a new row. All of this is true of a hold made in place too
(*In-place waive, cancel and hold*, above): releasing it is resolving its incident, and that
is not a recorded act.

**The rows of an act made in place.** A waive, a cancel or a hold of one instance writes the
same kinds of row, with `origin: "in_place"` where a migration's say `migration`, and a
`run_id` of its own where a migration's rows share one:

| Act | Row | Reaches | What it records |
| :-- | :-- | :-- | :-- |
| `waive` | `waive` | the task | the tasks withdrawn, the 200 with the lowest ids, as they were (status and holder) and as they are (`canceled`); the values the waive set (`after.variables`) and what the instance held under those names before (`before.variables`); in `details`, how many tasks were withdrawn (`withdrawn`), how many of them the row names (`tasks_listed`) and how many places decide from the step (`decision_points`, a count), and `control: true` when the step is marked as a control |
| `cancel` | `cancel` | the instance | its status, `active` to `cancelled`; the tasks withdrawn, the 200 with the lowest ids; the incidents closed, `open` to `resolved`, at most 200; in `details`, always, `withdrawn`, `tasks_listed`, `external_tasks_withdrawn` and `incidents_closed` |
| `hold` | `hold` | the instance | the incident (`after.incident`), and in `details` whether this hold raised it or found it open (`incident_raised`) |

A row names its step (`node_id`, `node_name`), except the row of a cancel that named none,
and its task (`task_id`) when exactly one task was withdrawn. Two things differ from a
migration's rows on purpose. A migration's cancel lists every task it withdrew, however
many, and writes the two counts only when they are not zero: its rows are pinned as they
were. And a migration's hold of a step that already has an open incident writes nothing
more, where a hold in place writes its row and says the incident was already there.

For more than 200 open tasks — on the instance for a cancel, on the step for a waive of a
repeating approval — the row does not name every holder
(`TestACancelOfMoreWorkThanAPlanListsWithdrawsAllOfIt`,
`TestAWaiveOfMoreRunsThanARowNamesWithdrawsThemAll`).
The holder of a task it does not name is on the task's own row, which a withdrawal changes
only the status of, and in the notice sent to them. The trail's entry for each withdrawal
names the step and not the holder.

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

**Under a second administrator's approval, running it again is asking again.** An approved
request is spent by its run, however the run ended. What remains needs a new request and a
new approval:

- **A run that stopped part-way.** The approver gets a 500 whose message ends *what its run
  had done stands, and what remains has to be asked for again*, and the request reads
  `interrupted`. Preview the migration again to see which instances remain, and apply it
  again: that makes a fresh request
  (`TestARunThatFailsAfterActingOnOneInstanceIsFinishedUnderAnotherRequest`).
- **A run that passed instances over.** The request reads `applied`. The reply to the
  approval lists who was passed over and why, to whoever approved, and to nobody else: it
  is not kept on the request, which holds only the count. Apply the migration again for
  those that remain, as a new request.
- **A request narrowed to named instances.** An instance the first run already moved is no
  longer on the version being migrated from, and a plan that names it is refused: *version
  1 of "quotation-approval" is not running instance(s) 0199…*. Leave it out of `instances`
  when asking again.
- **A repeating step nobody does by hand.** A skip of a repeating step that is not a user
  or manual task — one parked for an outside worker, say — skips one run of the step and
  passes the instance over
  (`TestASkipThatLeavesPartOfARepeatingStepBehindDoesNotMoveItToAVersionWithoutTheStep`).
  Asking again does not finish it. Seen once with a test that was not kept: the step's
  tokens never left it, and from the third run of the migration on, each run put one more
  token on the step after it. Do not skip such a step; cancel the instance or hold it.
  This is in the roadmap.

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
- `server/domains/services/impl/migration_second_approver.go`,
  `migration_renamed_in_place.go` — what a plan asks a second administrator for;
  `migration_gate.go`, `migration_approve.go` — the gate and the three steps of an approval
- `server/domains/services/impl/instance_deviation.go` — the waive, cancel and hold of one
  instance in place: the lock, the replay and what an apply asks of the locked row;
  `instance_deviation_request.go`, `instance_deviation_approve.go` — asking for a waive,
  and approving one
- `server/domains/services/impl/deviation_request_admit.go` — who may decide a request
- `tests/bpmn/instance_waive_test.go`, `tests/bpmn/instance_cancel_hold_test.go` — what
  each does; `tests/deviation/deviate_route_test.go` — who may ask
- `tests/instancemigration/state_test.go` — every case in §2 that has a test
