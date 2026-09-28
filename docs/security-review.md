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
  in 0.4.0). Editing a task somebody else holds, and reading and clearing
  somebody's notifications, took only a login until this cycle; any other
  endpoint that takes a person or a record from the request without asking
  whose it is is a suspect.

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

1. **Manual tasks are anybody's** in their organization: the designer has no
   field to name anybody for one. A product decision is pending.
2. **A service's own refusal over Connect is an HTTP 200** with the reason in
   the reply's `error` field, so monitoring does not see it and a client that
   ignores the field takes it for success.
3. **Messages published to RabbitMQ are transient**, and are lost if the broker
   restarts before delivering them.
4. **An external task's lock cannot be extended.** Work that outlasts
   `lock_seconds` is handed to another worker and can run twice.
5. **Redaction misses some secret names** — `client_secret`, `id_token`,
   `db_password` — whose key has a prefix before the word it matches.
6. **Organization-wide queries carry every project id** of the organization:
   ten thousand at the largest measured, a cost that grows with the tenant.

## Out of scope

- What [`SECURITY.md`](../SECURITY.md) lists under *What has already been
  looked at*, unless a regression is suspected.
- The deliberate decisions in `SECURITY.md`, as decisions — how they are
  implemented is in scope.
- Denial of service by volume against the environment itself.
- Production, and anything outside the environment above.
