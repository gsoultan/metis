# Security review: scope for 0.4.0

What an external reviewer is asked to look at, in the order it matters, and what
they can skip. [`SECURITY.md`](../SECURITY.md) is the reading to do first: it says
what is untrusted, which decisions are deliberate, and what earlier audits have
already covered.

## What is under review

- **The build:** the `v0.4.0` release candidate image, by digest — not a branch,
  so the findings apply to something that can be verified
  ([Releasing](releasing.md)).
- **An environment like production:** PostgreSQL; an OIDC provider with
  `METIS_OIDC_ORGANIZATION_CLAIM` filled; a RabbitMQ broker with one bridge and
  one consumer switched on; two organizations, each with an account for every
  role, and one account that belongs to both.
- **The source**, at the commit the image was built from.

Findings go through [private vulnerability
reporting](https://github.com/gsoultan/metis/security/advisories/new), each with
what an attacker gets, who has to be able to do what, and something that runs —
a request, a definition, a test. The ten findings `SECURITY.md` lists were each
found that way — by running something against hostile input — while tests over
them passed.

## In scope, in order

### 1. Reaching another organization's data

The worst outcome for a multi-tenant engine. 0.4.0 changed how an account's
memberships are read and how a request is scoped.

- **Choosing the tenant.** `X-Organization-ID` is checked against the caller's
  memberships (`server/interceptors/tenant/resolver.go`). For an account from
  the identity provider, memberships are what the token's organization claim
  says, and each sign-in makes them match it — new in 0.4.0.
- **Scoping every read and write.** The repository scope fails *open* when a
  context carries no tenant, so background work can span tenants
  (`internal/pkg/tenantscope`); `METIS_FEATURE_STRICT_TENANT_SCOPE` closes it
  and ships off. 0.4.0 scopes a request by one read of its organization's
  project ids. Wanted: any request path that reaches rows of an organization the
  caller did not select, and any background path that runs as the system with
  a request's input.
- **Roles held in one organization**, new in 0.4.0: the role check counts the
  caller's global roles and the ones held in the organization the tenant
  resolver chose, and nothing else. Wanted: a role granted in one organization
  that acts in another — through a route whose organization comes from the
  path or the body rather than the resolver, a global object such as an
  account, or a cached principal after a role is revoked.
- **Things that belong to an organization.** Connectors and groups each had a
  cross-organization flaw fixed this cycle (`CHANGELOG.md`, Security); their
  siblings — webhooks, environments, forms, decisions — are the likeliest next
  ones.

### 2. Becoming somebody else, or more than you are

- **Two kinds of token.** A local token (HMAC, `JWT_SECRET`, no issuer) and an
  identity provider's ID token (public key) are both accepted, each checked by
  its own rules alone (`server/interceptors/auth/token_kind.go`). Wanted:
  algorithm confusion, a token checked by the wrong rules, a key or issuer the
  provider did not publish, linking a first sign-in to an existing account.
- **Recovering an account.** `--reset-password` refuses an account linked to
  the identity provider; a password change ends the account's sessions; sign-in
  attempts are throttled (`internal/pkg/loginthrottle`).
- **Role checks.** `server/interceptors/auth/rbac.go` and `platform.go`. REST
  and Connect serve the same endpoints (`server/endpoints`), so a Connect method
  passes its REST twin's checks; `tests/endpointwiring` holds every declared
  endpoint to having a chain. A Connect refusal now carries its code rather than
  `unknown`. Wanted: a route or method that reaches a service around its chain.
- **Work that belongs to somebody.** Claiming, completing and handing over
  tasks: a task that names nobody is an administrator's or an operator's (new
  in 0.4.0). Handing a task over is decided by the service on the task's
  locked row (`server/domains/services/impl/task_handover_step.go`,
  `task_handover_target.go`): who may, whether they said why, and whether the
  person it goes to is in the organization, offered the step and not barred by
  separation of duties. Wanted: a hand-over that reaches somebody those checks
  should have refused, or one that leaves no audit entry. Editing a task
  somebody else holds, and reading and clearing somebody's notifications, took
  only a login until this cycle; any other endpoint that takes a person or a
  record from the request without asking whose it is is a suspect.
- **A second administrator.** A waive of a step of one instance, and a
  migration that skips a step, does not carry a control across, redirects a
  step past a control or takes a separation-of-duties rule away, are recorded
  as a request and made only when a different administrator of the
  organization approves (`server/domains/services/impl/deviation_request_admit.go`
  for who may; `migration_gate.go` for the check a migration's run makes of
  the stored request; `migration_second_approver.go` for what a plan asks
  about). The requester and the approver are told apart by account id. Wanted:
  a call, a route or an in-process path that makes one of those changes on one
  account's authority; an approval that carries out something other than what
  was asked; a request decided twice; a way to approve one's own request in an
  organization `METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS` does not name, or in a
  named one that has a second administrator; a request, header or setting an
  organization controls that adds to that list. Three paths round a control
  through a mapping were found and closed while this was built
  (`CHANGELOG.md`, Fixed); a fourth is the likeliest next finding.

  **What this control is not**, so that it is not reported as a finding and is
  not relied on for more: it does not protect against an administrator who
  creates, removes or displaces accounts, and it does not review the whole
  difference between two versions.
  [What it does not review](process-change-in-flight.md#what-it-does-not-review)
  and
  [What it does not protect against](process-change-in-flight.md#what-it-does-not-protect-against)
  list both in full.

### 3. Process definitions, which are untrusted input

Whoever can model a process is not whoever runs the engine.

- **Expressions.** FEEL for conditions and mappings
  (`server/domains/logic/feel`); decision tables, whose cells can now name other
  inputs of the decision (new in 0.4.0); `js:` conditions refused unless
  `METIS_FEATURE_JAVASCRIPT_CONDITIONS` is on; form logic evaluated by a bounded
  evaluator in the approver's browser.
- **Script tasks** run JavaScript in a sandbox (`server/domains/logic/sandbox.go`)
  under a wall-clock budget that cannot pre-empt a native call — a known limit.
- **Outbound requests.** Service task URLs and connectors become requests; the
  egress policy is enforced on the resolved address just before connecting
  (`internal/pkg/httpclient`). Wanted: redirects, DNS rebinding, IPv6 and
  IPv4-mapped addresses, proxy settings. The SMTP and AMQP connectors dial
  their hosts themselves, not through that client; running a connector with a
  configuration the caller writes took only a login until this cycle. Wanted:
  another way to point one at a host inside the network. The database lookup
  connector (`server/domains/services/impl/sqlconnector`) runs read-only with a
  statement timeout and row and byte caps: wanted, anything that escapes those.
- **Connector documents and OpenAPI imports**, which define connectors: one could
  take over a built-in connector until this cycle.

### 4. The browser

The session token is kept in `localStorage` (`ui/src/store/useAuthStore.ts`), so
script injection anywhere in the UI is account takeover. Wanted: any authored or
stored text that reaches the page as markup — BPMN diagrams and their
documentation, form definitions, decision tables, notification and incident
text, connector names.

### 5. Secrets

- **At rest:** AES-GCM (`internal/pkg/crypto`); the sealed audit trail and its
  reseal after a key rotation (`server/repositories/pg/sealed.go`,
  `internal/app/reseal.go`); configuration secrets
  (`internal/pkg/configsecret`).
- **On the way out:** two connector passwords were sent to the browser until
  this cycle; errors and logs pass through `internal/pkg/redaction`.
- **Webhook signatures** (`internal/pkg/webhooksig`): v2 with a replay window,
  and legacy signatures for ninety days after the upgrade.

### 6. Staying up

Request size, rate limits shared across replicas, backpressure, the idempotency
cache, SSE fan-out and paging bounds (`server/interceptors/security`); the
RabbitMQ bridge and consumer under a broker that stalls, closes channels or
disappears.

### 7. What is published

The release workflow (`.github/workflows/release.yml`): tag-only, keyless
signing, provenance and an SBOM for the image and the archives. Dependencies:
Go (`govulncheck` runs in CI) and the UI's (`ui/bun.lock`).

## Known and open

Found in this cycle and not yet changed. Confirming, rating or disproving these
is part of the review.

1. **A service's own refusal over Connect is an HTTP 200** with the reason in
   the reply's `error` field, so monitoring does not see it and a client that
   ignores the field takes it for success.
2. **Organization-wide queries carry every project id** of the organization:
   ten thousand at the largest measured, a cost that grows with the tenant.
3. **A message sets any variable it carries.** `SendMessage` is open to any
   signed-in member and writes whatever variables the message carries into the
   instances waiting for it (`engine.triggerSubscription`); `BroadcastSignal`,
   an operator's, does the same. It is the shape a task completion had before
   it was held to its form.
4. **A change to who administers has no durable record.** Creating an
   account, changing its roles and deleting it write one line each to the
   server's log, naming who did it, and nothing else: no trail, no table, and
   a membership carries no dates. A sign-in through the identity provider that
   changes an account's organizations, and a password reset from the command
   line, are not logged at all. So an administrator who creates a second
   administrator account, approves their own request as it and deletes it
   leaves an ordinary second approval in the ledger, and the only evidence is
   in the log. A durable trail of account changes, with a refusal of a
   self-approval that follows a recent change of roles, is the planned fix.
5. **The second administrator does not see everything a migration loosens.**
   Wider candidate groups, a lower completion condition, a changed gateway
   condition, the same step ids rearranged with a control moved behind an
   instance, and an unmarked step that carries a rule removed outright are not
   detected, and such a migration applies on one administrator's call.
6. **A waive that waits is found stale only when somebody tries to approve
   it**, and the plan an approver reads is the one the requester was shown.
   An approver who does not preview the instance again approves on what was
   true when it was asked.

## Out of scope

- What [`SECURITY.md`](../SECURITY.md) lists under *What has already been
  looked at*, unless a regression is suspected.
- The deliberate decisions in `SECURITY.md`, as decisions — how they are
  implemented is in scope.
- Denial of service by volume against the environment itself.
- Production, and anything outside the environment above.
