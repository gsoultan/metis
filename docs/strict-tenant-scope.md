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

`make strict-scope` runs seven suites with the flag forced on — every one of
them exercising product paths through the real interceptor chain, not through a
test shortcut:

```
tests/strictscope  tests/slo  tests/user  tests/setup  tests/outage
tests/replicas     tests/auth
```

That covers the HTTP surface, the job worker firing a timer, message
correlation, setup, and multi-replica behaviour. It is part of `make gate`, and
`tests/ci` fails if the list shrinks or the gate stops running it.

**What it does not cover** is anything that never goes through the interceptor
chain or a background worker under test. That residue is what the staging step
below is for.

> Running the *whole* suite under the flag reports around 130 failures. They are
> not product defects: most tests call services directly with a bare
> `t.Context()`, which carries no identity, where production always arrives
> through the auth interceptor and the tenant resolver. A red run nobody can act
> on is a run people learn to ignore, which is why the target names packages
> rather than `./...`.

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
