# Changelog

Notable changes to Metis BPM. Entries describe what changed for somebody
running this, and name the behaviour that changed rather than the files.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- **UI dependencies are audited in CI.** The Go side has had `govulncheck` from
  the beginning and the UI side had nothing. A first run reported 23 advisories,
  thirteen of them high — and every one was fixable within its existing range,
  which is the point: they were not unfixable, nobody had looked. Most were
  build-time (eslint, babel, vite) rather than shipped code, which lowers the
  severity and not the argument, since a compromised build tool ships
  compromised assets.

### Added

- **A security policy** ([`SECURITY.md`](SECURITY.md)). This repository is
  public and had no private way to report a vulnerability, so a finder's only
  option was an issue — which is a disclosure. It also states what counts as
  untrusted input, which deliberate decisions look alarming and are not, and
  what has already been audited.


## [0.1.4] - 2026-09-04

Two security fixes, both found by auditing rather than reported. The first is
the most serious this project has had: **upgrade past 0.1.3 if you accept
process definitions from anyone you would not give an approver's session to.**

### Security

- **A process definition could run JavaScript in the browser of whoever opened
  its task.** A user task's `form_definition` is a property of a node in a
  deployed definition, and its fields carry `logic.hiddenIf`,
  `logic.disabledIf`, `{{ … }}` defaults and a `validation.customJs` rule. All
  four were handed to `new Function` inside a `with` block and executed.

  The victim is normally an approver — by the nature of approval, someone with
  more authority than whoever modelled the form — and the code ran in their
  session, where the auth store and its token are reachable. Demonstrated
  against the previous build: a `hiddenIf` of
  `(globalThis.x = token, false)` read the session token, and the form rendered
  as though nothing had happened.

  The server has refused authored JavaScript in gateway conditions by default
  since FEEL landed, for exactly this reason. The browser is not a safer place
  to run it; it is where the session is.

  Form logic now goes through a bounded evaluator that can express comparisons,
  boolean logic and arithmetic over `data` and `vars`, and cannot express a
  function call, a property outside those two objects, an assignment, or any
  route to a global. A rule it cannot parse is refused and treated as false —
  the field stays visible and editable — rather than falling back to something
  that can run it. A test fails the build if `new Function` or `eval` reappears
  anywhere in the UI.

  **A `customJs` rule that is a comparison keeps working. One that is a program
  does not**, and the browser console says which rule was refused.


### Security

- **A connector's credentials could be written into an incident in plaintext.**
  A failed connector call carries the URL it was calling, and Go's `*url.Error`
  includes the query string — so a manifest that passes its API key as a query
  parameter, which many APIs require and which manifests are built to template,
  wrote that key into the incident table and the log on every connection
  failure. Incidents are kept and shown in the UI.

  The job service now redacts before storing or logging. The redactor already
  existed, already had patterns for `api_key=` and for URL userinfo, and was
  already applied to transport responses and setup — it was simply not called on
  the one path where a connector error becomes durable.


## [0.1.3] - 2026-09-03

Both entries below were found reviewing this session's own changes rather than
by a report — the setup one is a defect introduced by the secret validation in
0.1.2, and it is worth upgrading past.

### Fixed

- **The setup wizard could write a configuration the server refused to start
  with.** It applied a weaker rule than startup — sixteen characters for the
  encryption key, and nothing at all for the JWT secret beyond being present —
  so a wizard run could report success, save the config, and leave an
  installation that never came back after its first restart. Configured
  successfully and permanently unable to boot is a worse outcome than the weak
  secret the check was meant to prevent.

  Both paths now share one rule, and the wizard applies it *before* anything is
  encrypted with the key — which is the only moment the key can still be
  changed freely. `METIS_ALLOW_WEAK_SECRETS` is honoured in both places, so they
  cannot disagree in the other direction either.

- **Publishing an SSE event no longer starts a goroutine per event.** Fine while
  the database keeps up and unbounded when it does not: measured, five thousand
  events against a slow bus produced five thousand goroutines, each holding a
  payload and a pending write. The conditions that make a database slow are the
  conditions that make an engine busy, so it arrived when there was least room
  for it. Events now go through a bounded queue drained by a small pool, and
  what does not fit is dropped and counted — the same trade the local delivery
  already makes for a slow browser, and reported in the log rather than silently.


## [0.1.2] - 2026-09-03

### Security

- **Changing a password now ends every existing session.** It did not. The old
  password stopped working while every token minted with it stayed valid for the
  rest of its 24-hour life — so somebody changing their password *because they
  believed they were compromised* achieved nothing against the attacker already
  holding a session, and was told the change succeeded. Confirmed against the
  previous build: a token captured before the change still returned 200
  afterwards.

  Tokens issued before an account's last credential change are refused, for
  both the self-service change and `--reset-password`. The signed-in user is
  signed out too and asked to sign in again — that is the point rather than a
  side effect.

  Accounts that have not changed their password since upgrading are unaffected:
  the migration leaves the cutoff empty rather than filling it in, because
  filling it in would sign out every user on the installation at the moment of
  the upgrade.

### Added

- **The alerting rules are unit-tested.** `promtool check rules` proves the
  PromQL parses, not that it matches anything — a label that does not exist or a
  threshold on the wrong side parses perfectly and stays silent forever. Each
  rule is now asserted to fire on data that should trigger it and stay quiet on
  data that should not.

  Writing those tests found `MetisDown` matching `up{job=~".*metis.*"}`, which
  means an operator whose scrape job is named anything else gets an alert that
  never fires. `MetisMetricsMissing` is the backstop: it keys on a metric only
  Metis exports, so it holds whatever the job is called. **If it fires while
  `MetisDown` is silent, your scrape job is misnamed and `MetisDown` is not
  protecting you.**


## [0.1.1] - 2026-09-02

### Fixed

- **The published image is now `linux/amd64` and `linux/arm64`.** 0.1.0 shipped
  amd64 only, so Graviton and Ampere nodes — ordinary Kubernetes hardware — and
  every Apple Silicon developer had nothing to pull. The Dockerfile
  cross-compiles rather than emulating, so the second architecture costs almost
  nothing to build, and CI now builds it on every merge rather than leaving it
  to be discovered at tag time.

- **The release workflow could not publish at all.** It asked buildx for a
  provenance attestation, which the default `docker` driver cannot produce, so
  0.1.0 tagged and published nothing until the driver was set up. The workflow
  had never run before that tag: a release pipeline is not verified by existing.

## [0.1.0] - 2026-09-02

The first tagged release, and the first published image
(`ghcr.io/gsoultan/metis:0.1.0`).

**0.1.0 rather than 1.0.0, deliberately.** A single engine replica is the
supported topology — HTTP rate limiting and connector rate limits/circuit
breakers still hold per-process state, so with N replicas each of those limits
applies N times over ([`docs/recovery.md` §2.1](docs/recovery.md)). The strict
tenant scope, which makes a query carrying no identity return nothing instead of
everything, ships off pending a staged rollout
([`docs/strict-tenant-scope.md`](docs/strict-tenant-scope.md)). Both are written
down rather than discovered, and both are reasons the HTTP API and the metric
names are not yet being promised as stable.

What that number does *not* mean is untested. Read latency was measured at
500,000 instances and 500,000 tasks across 50 tenants and stayed under 5.2ms
against a 150ms target; the backup and restore procedure was rehearsed end to
end, including reading an encrypted variable back after a `DROP SCHEMA CASCADE`;
and every merge runs the suite against SQLite, PostgreSQL, MySQL and SQL Server,
builds the production image and boots it.

Everything below this heading shipped in it.

### Added

- **Signed-in users can change their own password**, from Profile → Change
  Password (`POST /api/v1/users/me/password`). Previously the only way was
  `--reset-password` from a shell on the server, so every rotation on a running
  installation needed access to the machine — including the administrator
  account the setup wizard creates, which could never be rotated by its owner.

  It asks for the current password, and that is the point rather than a
  formality: without it a stolen session token would be enough to lock the real
  owner out of their own account permanently. Accounts that sign in through
  OIDC are refused — their password lives at the identity provider, and Metis
  holds no hash their login consults, so "changing" it here would report
  success while altering nothing.

- **CI builds the image and boots it.** Nothing built the Dockerfile before,
  though the README calls it the supported artifact. The job now starts the
  built image against a real PostgreSQL with a read-only root filesystem and
  waits on `/readyz` — because the image is distroless, the failures that matter
  (a dynamically linked binary, a missing CA bundle, a moved entrypoint) all
  produce something that builds cleanly and exits on first run.

### Changed

- **GoBPM is now Metis.** The module path is `github.com/gsoultan/metis`, the
  binary and container entrypoint are `metis`, and the repository moved with
  them. Existing installations keep running without reconfiguration: `GOBPM_*`
  environment variables are still read (with a warning naming the replacement),
  an existing `gobpm.db` is still opened, and browser sessions are migrated
  rather than dropped. See [`docs/upgrading.md`](docs/upgrading.md) for what to
  change and when the fallbacks go.

  The one thing that does need editing is the import path, and the Go client's
  package name along with it — `gobpm.NewClient` is now `metis.NewClient`.

  **Metrics and traces are renamed with no fallback.** They are now
  `metis_http_requests_total`, `metis_http_request_duration_seconds`,
  `metis_http_requests_in_flight`, and the OTel service, span and attributes are
  `metis.*`. A dashboard or alert rule still written against `gobpm_` does not
  error — it matches nothing and stops firing, which is harder to notice than a
  break. Grep your alerting rules before upgrading.

  The `Idempotency-Key` sent on outbound service calls still begins with
  `gobpm-`, deliberately and permanently. It is derived fresh on every retry, so
  it is the only thing telling a downstream system that a retry is the request
  it already handled; renaming it would make a job caught mid-retry by the
  upgrade arrive looking new, and charge the card twice.

### Security

- **A weak `ENCRYPTION_KEY` or `JWT_SECRET` is refused at startup.** Both were
  accepted on the single condition of being non-empty, so `JWT_SECRET=secret`
  started a server whose administrator tokens could be forged offline.

  Both must now be at least 32 characters — what `openssl rand -hex 16`
  produces, which is what the documentation has always recommended — and must
  not be one of the placeholders published in this repository. That last check
  matters more than the length one: the evaluation `ENCRYPTION_KEY` in
  `docker-compose.yml` is exactly 32 characters, so it passes every length rule
  while being known to anyone with the repository, and a compose file is the
  easiest thing to carry from an evaluation into production.

  **Upgrading with a weak key is a real possibility, and `ENCRYPTION_KEY` cannot
  simply be rotated** — changing it does not re-encrypt anything, it makes
  existing variables unreadable. `METIS_ALLOW_WEAK_SECRETS=true` starts the
  server anyway, warning on every boot, so the remedy is a planned
  re-encryption rather than an outage.


- **Service tasks could reach internal addresses through a hostname.** The
  egress guard checked IP ranges, but only when a URL literally contained an IP:
  for a hostname `net.ParseIP` returned nil and the checks were skipped
  entirely. A process definition — untrusted input — naming a host whose address
  record pointed at `169.254.169.254` reached the cloud metadata endpoint, and
  one pointing at `127.0.0.1` reached anything bound to loopback. Confirmed
  against the previous code by reading the body of a loopback-only server
  through a public hostname.

  The check now runs on the resolved address immediately before connect, which
  also closes DNS rebinding: there is no second lookup between the check and the
  connection. `100.64.0.0/10` is refused as well — Go's `IsPrivate` does not
  cover it, and several Kubernetes CNIs put internal service networks there.

  A host named in `METIS_HTTP_ALLOWED_HOSTS` still reaches its private address:
  that is what the setting is for.


- **The HTTP rate limiter could be bypassed by setting a header.** The limiter
  keyed its buckets on `X-Forwarded-For` and believed it unconditionally. That
  header is set by the client, so sending a different value on every request
  bought a fresh allowance every time: measured against the previous code, one
  address took 30 requests through a limit of 3, and only stopped because the
  test stopped.

  The header is now consulted only when the request arrived from a peer allowed
  to set it — loopback and private space by default, configurable with
  `METIS_TRUSTED_PROXIES`, and `none` for a directly-exposed server. Anything
  else is charged to the address it actually connected from.

  **If your load balancer sits in public address space, set
  `METIS_TRUSTED_PROXIES` to its range**, or every client behind it will share
  one bucket.


- **`js:` gateway conditions are refused by default.** The JavaScript runtime
  behind them cannot be memory-bounded — a single native call ran for 37s
  against a 200ms budget, allocating throughout — so a default installation was
  one deployed definition away from memory exhaustion. FEEL has evaluated
  conditions natively since the expression layer landed.

  **Upgrading:** a definition still using `js:` will refuse to route, loudly,
  with an error naming the condition. `GET /api/v1/definitions/javascript-conditions`
  lists every affected definition. Set `METIS_FEATURE_JAVASCRIPT_CONDITIONS=true`
  to keep the old behaviour while migrating.

- **Idempotency keys are scoped to the caller.** The cache was keyed on method,
  path and the header value alone. Because clients choose their own keys and
  choose obvious ones, two tenants using the same value shared a cache entry:
  the second was served the first's response body and its own write never ran.
  Keys are now namespaced by tenant and user.

- **A malformed identifier is a 400, not a 500.** Forty-two endpoints returned
  the raw parse error for a bad UUID, which the transport mapped to a server
  error — spending the 0.1% 5xx error budget on client typos and paging whoever
  was on call for requests answered correctly.

### Fixed

- **FEEL's `matches()` was not a regular expression.** It matched literally, so
  a decision table written with `matches(code, "^ERR-[0-9]+$")` did not error
  and did not warn — it answered false for every input and the process took the
  other branch. A rule that never fires is a wrong answer delivered
  confidently, not a missing feature.

  It matched literally to avoid catastrophic backtracking on a pattern taken
  from a deployed definition. That reasoning does not apply to Go, whose
  `regexp` is RE2 and does not backtrack: the textbook `^(a+)+$` case measures
  119µs rather than hanging.

  **Patterns containing no metacharacters behave exactly as before** — Go's
  matching is unanchored, so a literal pattern is still a substring test. A
  definition using a real pattern starts working, which is a behaviour change if
  you were relying on the rule never firing. An uncompilable pattern is now an
  error rather than a silent false.


- **A client retry landing on another replica no longer re-executes the write.**
  Idempotency records were held in the serving process, so a second replica
  found an empty cache and ran the work again — a duplicate business action,
  which is what the header exists to prevent. Records now live in
  `idempotency_records` (migration 8), claimed with a single conditional insert
  so exactly one replica executes and the rest replay its answer. Proven across
  SQLite, PostgreSQL and MySQL.

  This closes the only multi-replica gap that could corrupt data. Four remain
  and are degradations rather than corruption — see `docs/recovery.md` §2.1.

- **The PostgreSQL advisory lock never released.** Advisory locks are
  session-scoped and the locker held a connection *pool*, so the release ran on
  a session that held nothing, reported success, and left the lock held on an
  idle pooled connection. It now pins a session per lock.

- **A job could be claimed and then stranded.** The claim wrote a five-minute
  lease before taking the distributed lock, so a refused lock left a job marked
  running that nobody was running. The lock is now taken first, and released if
  the row race is then lost.

- **The bundle budget was calibrated on the wrong platform.** The same tree
  measures ~2.4 kB larger on Linux than macOS, against a budget with 1.5 kB of
  headroom — so the check passed on a laptop and blocked the release image.

### Added

- **Connector manifests can authenticate with OAuth client credentials.** The
  schema accepted `oauth2_client_credentials` and the runtime then refused it,
  so a manifest could be written, validated and installed and would fail on its
  first call. Tokens are fetched once and reused until shortly before expiry;
  concurrent callers share one fetch rather than each starting their own.

- **A production image.** Three stages (bun builds the UI, Go builds a static
  binary, distroless runs it as non-root on a read-only root filesystem), plus
  a compose file for evaluation. `make docker`, `make docker-run`.

- **`/healthz` reports the running build**, so "which version is this" no longer
  depends on finding a startup log line.

- **Backup and restore scripts** (`scripts/backup.sh`, `scripts/restore.sh`)
  implementing the procedures in `docs/recovery.md`. The backup refuses to run
  without `ENCRYPTION_KEY`; the restore refuses to start until the operator
  confirms the engine is stopped.

- **The strict tenant scope says which path forgot an identity.** It answers an
  unidentified query with nothing rather than an error, which is the right
  contract but made the flag hard to adopt: a background path that forgets to
  mark itself does not fail, it goes quiet, and an operator evaluating it in
  staging had to notice an absence. Each denial now logs the repository method
  and its caller, once per site, so the rollout is reading a list instead.

- **CI now runs every dialect-gated suite, and a guard keeps it that way.**
  `tests/migrations`, `tests/replicas` and `tests/outage` need a real database
  but were absent from the dialects job, so they skipped for want of a DSN,
  reported ok, and were never run against PostgreSQL or MySQL by anything —
  including the conditional-insert claim that had a MySQL-only bug when it
  landed. `tests/ci` now fails when a dialect-gated package is missing from that
  job, so the next one cannot be forgotten silently.

- **Connector contract tests** (`tests/connector/contract_test.go`) — the test
  pyramid's last missing tier. They pin what each connector puts on the wire and
  how it reads what comes back, including that a manifest's error rules decide
  before its success condition, and that the outbound egress policy refuses a
  private address by default.

- **SLO tests** (`tests/slo`). The roadmap's targets now fail a build when
  missed. Measured against PostgreSQL 17: reads p95 11.1ms (target 150ms),
  workflow actions p95 13.8ms (target 500ms), 0.000% 5xx (budget 0.1%),
  170,569 process starts/min (target 10,000/min).

- **Strict-tenant-scope integration coverage** (`tests/strictscope`), entering
  through the real HTTP chain and job worker, so the flag can be turned on in
  staging with evidence behind it. `make strict-scope`.

### Changed

- **The supported topology is a single engine replica**, now stated in
  `docs/recovery.md` §2.1 along with what breaks with two and why. Job claiming,
  migrations, correlation and outbound-call idempotency are already
  replica-safe; rate limiting, the idempotency cache, SSE delivery, connector
  breakers and the AMQP bridge are not.

- The designer's condition builder emits FEEL rather than JavaScript, and shows
  an expression too complex for its three fields instead of silently replacing
  it.
