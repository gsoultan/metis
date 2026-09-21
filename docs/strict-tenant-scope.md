# Turning on the strict tenant scope

`METIS_FEATURE_STRICT_TENANT_SCOPE` makes a repository query that carries
neither a tenant nor a system identity return **nothing** instead of
**everything**.

It ships off. This page is how to turn it on without breaking anything, and how
to know you have succeeded.

## Why it is off, and why that matters

With the flag off, a code path that fails to resolve a tenant gets unscoped
access — every organization's rows. That contradicts the rule the rest of the
codebase is held to (`AGENTS.md` §2.3: absent constraint means deny), and on a
multi-tenant installation it is the difference between a bug and a disclosure.

So why not simply default it on? **Because its failure mode is silence.** A
background worker that forgets to mark itself as system work does not error. It
reads no rows, and an engine that reads no rows looks exactly like an engine
with no work to do. Timers stop firing. Nothing appears in a log. An operator
watching a staging environment has to notice an *absence*, and absences are what
people miss.

Everything below exists to turn "watch for something that stops happening" into
"read this list".

## Before you start: what is already proven

`make strict-scope` runs the **whole module** with the flag forced on, and it is
green. It is part of `make gate`, and CI runs the same thing.

That was not always true, and the change matters because it moves where the
remaining risk lives. This target used to name seven suites, because `./...`
under the flag reported around 175 failing tests across twelve packages: most
of the suite called services with a bare `t.Context()`, which carries no
identity, where production always arrives through the auth interceptor and the
tenant resolver. A red run nobody can act on is a run people learn to ignore.

Those tests now carry a resolved tenant. That was worth doing for its own sake
rather than to make a number go green — several of them were reaching rows that
belong to nobody: a group seeded into no organization, tasks in no project,
connector instances naming project ids that did not exist. They passed only
because the scope fails open, so they were asserting against a path no request
takes.

> **Measure it with a DSN.** Without `METIS_TEST_POSTGRES_DSN`, every package
> that needs a real database skips and still reports `ok`. An earlier pass on
> this page claimed the suite already passed module-wide on the strength of a
> run with no DSN set; it did not. See AGENTS.md §4 for the other two ways to
> be green on nothing.

**What a green suite still does not tell you.** It proves no path *under test*
loses its identity. It cannot prove that of a path nobody wrote a test for, and
the failure mode here is silence rather than an error — a background worker
that forgets its system marker reads no rows and raises nothing. That residue
is what the staging step below is for, and it is why the flag still ships off:
the gate is real traffic, not the unit suite.

## A soak has already been run

Against a real server on PostgreSQL with the flag on, 2026-09-04. It deployed a
definition with a timer, a gateway and a user task; started five instances; let
the job worker fire the timers; then swept 24 read endpoints and nine writes —
completing a task, creating users and projects, deploying and evaluating a
decision, fetch-and-lock, and a password change.

**No denials.** Not one path reached a repository without an identity.

Repeated 2026-09-20 against a seeded development installation on PostgreSQL,
driving the browser as well as the API: all fifteen UI routes, the reads behind
them, a process started from the Models page, a task claimed and completed,
signal and message correlation, fetch-and-lock, an OCEL export, the webhook
receiver refusing an unknown token, and the job worker claiming and retrying
service-task jobs throughout. **Again no denials and no 5xx**, with
`DeniedSites()` empty.

The detector was confirmed live rather than assumed, because a zero from a dead
detector looks exactly like a zero from a clean run and silence is this flag's
whole failure mode. Its own tests prove a denied query is recorded with its
caller, that a site is named once however often it runs, that nothing is
reported while the flag is off, and that system work is exempt.

> **Do not read a green `make strict-scope` as more than it is.** That same day,
> `go test ./...` with the flag on appeared to pass module-wide on a developer
> machine, which would have meant the note below was obsolete. It was not: the
> DSN was unset, so every package needing a real database skipped and reported
> `ok`. With `METIS_TEST_POSTGRES_DSN` set, the same command fails 13 packages,
> which is what CI had been saying all along. `make test`, `make race` and
> `make strict-scope` now warn when the DSN is missing. A skip and a pass are
> the same word, and that is the second time on this page that an absence has
> been mistaken for a result.

That is evidence and not a substitute for yours: the traffic was synthetic and
the installation was a fresh one. What it establishes is that the paths a
product exercise touches are already carrying identity, so a soak against your
workload is looking for the features this did not use rather than for a general
problem.

It also produced a fix worth knowing about. The flag used to resolve lazily, on
first use — and its only two uses are deep in request paths, so an installation
where nothing went wrong never logged what it had been configured with. Turning
it on and seeing nothing was indistinguishable from mistyping the variable. The
flags are now read at startup, so the log states them before anything serves:

```
INF Feature flag is not at its default enabled=true flag=strict-tenant-scope
```

**Check for that line first.** If it is absent, the flag is not on and
everything below is measuring the old behaviour.

## The rollout

**1. Turn it on in staging.**

```yaml
env:
  - name: METIS_FEATURE_STRICT_TENANT_SCOPE
    value: "true"
```

**2. Exercise the product.** Deploy a definition, start instances, let timers
fire, work the task inbox, run a decision, drive a connector, let a message
correlate. Breadth matters more than depth: you are looking for a path nobody
thought about, so use the features you actually use.

**3. Read the warnings.** Every denial logs once per call site:

```
WRN A repository query carried neither a tenant nor a system identity, so it was
    answered with nothing.
    repository=...gormProcessRepository.GetForUpdate
    at=server/repositories/gorms/process.go:54
    called_from=...(*Engine).GetInstanceForUpdate
    flag=METIS_FEATURE_STRICT_TENANT_SCOPE
```

`repository` says what came back empty. **`called_from` is the thing that has to
change** — it is the path that failed to carry an identity.

Once per site, not per occurrence: these sit on poll loops that run every couple
of seconds, and the useful output is the list of paths, not a count of how often
they ran.

**4. Fix each one.** Two possibilities, and picking the wrong one is how this
becomes a vulnerability rather than a fix:

- **Background work** that legitimately spans tenants — a worker, a consumer, a
  migration — takes `entities.WithSystemContext`.
- **Anything serving a request** needs a *resolved tenant*, not a system marker.
  Marking a request path as system work makes the warning go away and the
  cross-tenant access permanent. If a request cannot resolve a tenant, that
  request should fail.

Add a test alongside the fix. `tests/strictscope` is where it goes, and
`assertNothingWasDenied` is the assertion — it turns the same diagnostic into a
build failure.

**5. When no warnings appear, carry it to production.** Leave it on through a
full release cycle before treating it as settled.

## Checking without reading logs

`gorms.DeniedSites()` returns the same list as a value, which is what the tests
assert on. If you want it in staging without log-grepping, that is the hook to
expose.

## When it is done

Delete the flag and make the behaviour unconditional. It is recorded as
`Retire:` on the flag itself for exactly that reason — a feature flag with no
retirement plan becomes permanent configuration nobody dares change.
