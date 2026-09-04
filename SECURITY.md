# Security

## Reporting a vulnerability

**Use [GitHub's private vulnerability reporting](https://github.com/gsoultan/metis/security/advisories/new).**
It is the "Report a vulnerability" button under the Security tab.

Please do not open a public issue for a suspected vulnerability. This project is
public, so an issue is a disclosure, and it will be read by more people than can
fix it.

What helps most, in rough order:

- **What an attacker gets.** "Reads another tenant's variables" is worth more
  than a CVE class, because it decides how fast this has to move.
- **Who has to be able to do what.** Deploy a definition? Hold a session? Reach
  the network? The trust boundaries below say why that changes the answer.
- **Something that runs.** A request, a definition, a test — the ten issues
  found in this codebase were all found by running something, not by reading.

You will get an acknowledgement, and an honest answer about severity and
timing rather than an automatic one.

## Supported versions

Only the latest release. This project is pre-1.0 and the version numbers say so:
fixes go into a new patch release rather than being backported.

## What is untrusted

This is the part worth knowing before reviewing anything, because most of what
looks alarming here is deliberate and most of what looks routine is not.

**A process definition is untrusted input.** It is authored by whoever can model
a process, which is not the same authority as whoever runs the engine, and it
reaches:

- **Gateway and completion conditions**, evaluated as FEEL. `js:` conditions are
  **refused by default** — the JavaScript runtime behind them cannot be
  memory-bounded, measured at 37 seconds against a 200ms budget.
- **Service task URLs and connector configuration**, which become outbound
  requests. The egress policy is enforced on the *resolved address* immediately
  before connect, not on the hostname, because a name says nothing about where
  it points.
- **User task form definitions**, which reach the browser of whoever opens the
  task. Form logic is evaluated by a bounded expression evaluator, not by
  JavaScript — that was a real vulnerability, fixed in 0.1.4.
- **Script tasks**, which do run JavaScript, under a wall-clock budget that
  cannot pre-empt a native call. This is a known limitation, not an oversight;
  see `AGENTS.md` §2.3.

**Process variables are untrusted.** They come from API callers and from
messages, are encrypted at rest, and are templated into connector requests.

**Client headers are untrusted**, including `X-Forwarded-For`, which is only
believed from peers named in `METIS_TRUSTED_PROXIES`.

## Deliberate decisions a reviewer will otherwise flag

- **The repository tenant scope fails open** when a context carries no tenant
  identity, so that background workers can span tenants. `METIS_FEATURE_STRICT_TENANT_SCOPE`
  reverses it and ships off pending a staged rollout, because its failure mode is
  silence rather than an error. See [`docs/strict-tenant-scope.md`](docs/strict-tenant-scope.md).
- **A single replica is the supported topology.** HTTP rate limiting and
  connector rate limits/circuit breakers hold per-process state.
  [`docs/recovery.md` §2.1](docs/recovery.md) has the table.
- **The outbound `Idempotency-Key` still begins with `gobpm-`**, from before the
  rename. It is derived fresh on every retry, so it is the only thing telling a
  downstream that a retry is the request it already saw. Renaming it would
  double-execute anything caught mid-retry by an upgrade.
- **SSE delivery is best-effort.** A client whose buffer is full is skipped
  rather than waited for, and the UI treats an event as a hint to refetch.

## What has already been looked at

Ten issues were found and fixed by audit between 2026-08-30 and 2026-09-04, so a
reviewer can skip re-treading them. Each is in the changelog with what it was
measured to do.

Egress bypassable by hostname; the rate limiter disabled by a header; a stolen
session surviving a password change; secrets accepted that the server would not
start with; a setup wizard that could write an unbootable config; connector
credentials written into incidents; form logic executing authored JavaScript in
an approver's browser; and three resource bounds — unbounded goroutines,
unbounded SSE fan-out, and an unbounded pattern cache.

**Checked and found clean**, so also skippable: BPMN XML (XXE, entity expansion,
nesting — all refused by `encoding/xml` being strict, not by this code), JWT
handling (HMAC-only, expiry enforced, roles read from the database rather than
the token), AES-GCM at rest (random nonce per encryption, length-checked),
webhook signatures (constant-time), CORS (exact allowlist match), tenant
selection from `X-Organization-ID` (validated against real memberships), and the
connector templating engine (FEEL, not `text/template`; `config` and `input` are
separate scopes).

The most useful thing to know about that list: **every one of the ten had
passing tests over it.** None was found by a test going red. They were found by
asking what a specific piece of code would do with hostile input, and then
running it.
